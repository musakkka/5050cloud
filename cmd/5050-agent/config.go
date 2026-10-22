// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Default paths (docs/SPEC.md 6.4).
const (
	defaultConfig  = "/etc/5050/agent.json"
	defaultHostKey = "/etc/5050/host.key"
	defaultDataDir = "/var/lib/5050"
	defaultDocker  = "unix:///var/run/docker.sock"
)

// agentConfig is /etc/5050/agent.json (0600). It holds the host token, so
// it is never logged.
type agentConfig struct {
	ControlURL      string         `json:"controlUrl"`
	HostID          string         `json:"hostId"`
	HostToken       string         `json:"hostToken"`
	Roles           []string       `json:"roles,omitempty"`
	RegistryAddress string         `json:"registryAddress,omitempty"`
	Epoch           string         `json:"epoch,omitempty"`
	Mesh            proto.MeshSelf `json:"mesh"`
	// CAPin pins the console certificate ("sha256:<hex>") when it comes
	// from Caddy's internal CA (install --ca-pin).
	CAPin string `json:"caPin,omitempty"`
	// CACertFile is an extra CA certificate (PEM) for the console.
	CACertFile string `json:"caCertFile,omitempty"`
	PublicIP   string `json:"publicIp,omitempty"`
	JoinedAt   int64  `json:"joinedAt,omitempty"`
}

func loadConfig(path string) (agentConfig, error) {
	var c agentConfig
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("%s does not exist. Join this host first with 5050-agent join --url <console URL> --token <join token>", path)
	}
	if err != nil {
		return c, fmt.Errorf("could not read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if c.ControlURL == "" || c.HostToken == "" || c.HostID == "" {
		return c, fmt.Errorf("%s is missing controlUrl, hostId or hostToken. Run 5050-agent rejoin with a new join token", path)
	}
	return c, nil
}

func saveConfig(path string, c agentConfig) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", filepath.Dir(path), err)
	}
	return statecache.WriteFileAtomic(path, append(b, '\n'), 0o600)
}

// readMachineID reads /etc/machine-id (or the D-Bus copy).
func readMachineID(root string) (string, error) {
	if root == "" {
		root = "/"
	}
	for _, p := range []string{"etc/machine-id", "var/lib/dbus/machine-id"} {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	return "", errors.New("this host has no /etc/machine-id. Create one with systemd-machine-id-setup and try again")
}

func readCACert(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read the console CA certificate %s: %w", path, err)
	}
	return b, nil
}
