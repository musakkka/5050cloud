// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/core"
	"github.com/musakkka/5050cloud/internal/agent/core/engine"
	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/proto"
	"github.com/musakkka/5050cloud/pkg/version"
)

// joinParams are the inputs of join and rejoin.
type joinParams struct {
	URL        string
	Token      string
	Replace    string // ReplaceHostID (rebuild of a dead control host, 9.17.2)
	CAPin      string
	CACertFile string
	PublicIP   string
	ConfigPath string
	DataDir    string
	HostKey    string
	DockerHost string
	// Root prefixes /etc for the machine id (tests).
	Root string
	// PublicKey returns this host's WireGuard public key (netw generates it once).
	PublicKey func() (string, error)
	// Attempts and Wait control retries of network errors (tests shorten them).
	Attempts int
	Wait     time.Duration
}

// joinURL is <console>/api/agent/v1/join.
func joinURL(control string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(control), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("the console address %q must be a URL like https://console.example.com", control)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/agent/v1/join"
	return u.String(), nil
}

// doJoin registers this host with the control plane and writes agent.json.
func doJoin(ctx context.Context, p joinParams) (agentConfig, error) {
	var cfg agentConfig
	if strings.TrimSpace(p.Token) == "" {
		return cfg, errors.New("a join token is required (--token). Create one in the console under Hosts > Add host")
	}
	endpoint, err := joinURL(p.URL)
	if err != nil {
		return cfg, err
	}
	caPEM, err := readCACert(p.CACertFile)
	if err != nil {
		return cfg, err
	}
	client, err := core.HTTPClient(caPEM, p.CAPin)
	if err != nil {
		return cfg, err
	}
	client.Timeout = 30 * time.Second
	if _, err := statecache.LoadOrCreateKey(p.HostKey); err != nil {
		return cfg, fmt.Errorf("could not prepare the host key %s: %w", p.HostKey, err)
	}
	machineID, err := readMachineID(p.Root)
	if err != nil {
		return cfg, err
	}
	pub, err := p.PublicKey()
	if err != nil {
		return cfg, fmt.Errorf("could not create the WireGuard key of this host: %w", err)
	}
	hostname, _ := os.Hostname()
	var eng engine.Engine
	if d, err := engine.NewDocker(p.DockerHost); err == nil {
		eng = d
	}
	req := proto.JoinRequest{
		Token:    strings.TrimSpace(p.Token),
		Hostname: hostname,
		Facts: core.GatherFacts(ctx, core.FactSources{
			Root: p.Root, DataDir: p.DataDir, Version: version.Version, PublicIP: p.PublicIP, Engine: eng,
		}),
		MachineID:     machineID,
		WGPublicKey:   pub,
		ReplaceHostID: p.Replace,
	}
	resp, err := postJoin(ctx, client, endpoint, req, p.Attempts, p.Wait)
	if err != nil {
		return cfg, err
	}
	cfg = agentConfig{
		ControlURL:      strings.TrimRight(strings.TrimSpace(p.URL), "/"),
		HostID:          resp.HostID,
		HostToken:       resp.HostToken,
		Roles:           resp.Roles,
		RegistryAddress: resp.RegistryAddress,
		Epoch:           resp.Epoch,
		Mesh:            resp.Mesh,
		CAPin:           p.CAPin,
		CACertFile:      p.CACertFile,
		PublicIP:        p.PublicIP,
		JoinedAt:        time.Now().UnixMilli(),
	}
	if err := saveConfig(p.ConfigPath, cfg); err != nil {
		return cfg, fmt.Errorf("the host joined as %s but %s could not be written: %w", resp.HostID, p.ConfigPath, err)
	}
	return cfg, nil
}

// postJoin sends the join request. Network errors and 5xx answers are
// retried; a refusal (4xx) is final, because join tokens work once.
func postJoin(ctx context.Context, client *http.Client, endpoint string, req proto.JoinRequest, attempts int, wait time.Duration) (proto.JoinResponse, error) {
	if attempts <= 0 {
		attempts = 5
	}
	if wait <= 0 {
		wait = 3 * time.Second
	}
	body, err := json.Marshal(req)
	if err != nil {
		return proto.JoinResponse{}, err
	}
	var last error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return proto.JoinResponse{}, ctx.Err()
			case <-time.After(wait):
			}
		}
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return proto.JoinResponse{}, err
		}
		hreq.Header.Set("Content-Type", "application/json")
		hreq.Header.Set("X-5050-Agent-Version", version.Version)
		res, err := client.Do(hreq)
		if err != nil {
			last = fmt.Errorf("could not reach %s: %w", endpoint, err)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			var jr proto.JoinResponse
			if err := json.Unmarshal(b, &jr); err != nil || jr.HostID == "" || jr.HostToken == "" {
				return proto.JoinResponse{}, errors.New("the control plane answered the join with something the agent cannot read; check that the URL points at the console")
			}
			return jr, nil
		}
		msg := serverMessage(b)
		if res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests {
			last = fmt.Errorf("the control plane could not join this host (HTTP %d): %s", res.StatusCode, msg)
			continue
		}
		return proto.JoinResponse{}, fmt.Errorf("the control plane refused the join (HTTP %d): %s", res.StatusCode, msg)
	}
	return proto.JoinResponse{}, last
}

// serverMessage extracts the sentence of an api.Error body.
func serverMessage(b []byte) string {
	var e api.Error
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	if s == "" {
		return "no details were given."
	}
	return s
}
