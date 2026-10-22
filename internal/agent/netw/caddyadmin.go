// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

// caddyAdmin talks to Caddy's admin API over its unix socket. The socket is
// root-only (0600), so the agent is its only client.
type caddyAdmin struct {
	socket string
	hc     *http.Client
	loads  atomic.Int64 // successful POST /load calls, for tests and logs
}

func newCaddyAdmin(socket string) *caddyAdmin {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    2,
		IdleConnTimeout: 30 * time.Second,
	}
	return &caddyAdmin{socket: socket, hc: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

func (a *caddyAdmin) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Caddy's admin API on %s did not answer: %w", a.socket, err) //nolint:staticcheck // product name first
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return nil, fmt.Errorf("Caddy refused %s %s with status %d: %s", method, path, resp.StatusCode, msg) //nolint:staticcheck // product name first
	}
	return out, nil
}

// ping reports whether the admin API answers.
func (a *caddyAdmin) ping(ctx context.Context) error {
	_, err := a.do(ctx, http.MethodGet, "/config/", nil)
	return err
}

// config returns the running config, or nil when Caddy has none.
func (a *caddyAdmin) config(ctx context.Context) ([]byte, error) {
	b, err := a.do(ctx, http.MethodGet, "/config/", nil)
	if err != nil {
		return nil, err
	}
	if t := strings.TrimSpace(string(b)); t == "null" || t == "" {
		return nil, nil
	}
	return b, nil
}

// load replaces the whole config. stream_close_delay keeps open websockets
// and streams across it.
func (a *caddyAdmin) load(ctx context.Context, cfg []byte) error {
	if _, err := a.do(ctx, http.MethodPost, "/load", cfg); err != nil {
		return err
	}
	a.loads.Add(1)
	return nil
}

// sameJSON compares two JSON documents by value, ignoring formatting.
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
