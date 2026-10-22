// SPDX-License-Identifier: AGPL-3.0-only

// Package tools implements agentapi.Tools: it downloads the pinned helper
// binaries the control plane lists in HostConfig.Tools (railpack, wal-g,
// restic, litestream) into <DataDir>/tools/<name>-<version>, verifies the
// SHA-256 of the download, unpacks the binary from a .tar.gz when the tool
// names a path inside the archive, and returns the local path. A tool is
// downloaded once per version; concurrent callers share one download.
package tools

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/agentapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// MaxDownloadBytes caps one tool download.
const MaxDownloadBytes = 512 << 20

var safeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Manager fetches and verifies tools.
type Manager struct {
	dir    string
	client *http.Client

	mu       sync.Mutex
	tools    map[string]proto.Tool
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	path string
	err  error
}

// New returns a manager storing tools in dir (normally <DataDir>/tools).
func New(dir string, client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Manager{dir: dir, client: client, tools: map[string]proto.Tool{}, inflight: map[string]*call{}}
}

var _ agentapi.Tools = (*Manager)(nil)

// Set replaces the list of tools this host may use.
func (m *Manager) Set(tools []proto.Tool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = make(map[string]proto.Tool, len(tools))
	for _, t := range tools {
		m.tools[t.Name] = t
	}
}

// Known returns the configured tool by name.
func (m *Manager) Known(name string) (proto.Tool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tools[name]
	return t, ok
}

// Path returns the local path of a verified tool, downloading it first when
// needed.
func (m *Manager) Path(ctx context.Context, name string) (string, error) {
	m.mu.Lock()
	t, ok := m.tools[name]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("the tool %s is not configured for this host; the control plane lists the tools each host may use", name)
	}
	if !safeRe.MatchString(t.Name) || !safeRe.MatchString(t.Version) {
		m.mu.Unlock()
		return "", fmt.Errorf("the tool name %q or version %q contains characters that are not allowed", t.Name, t.Version)
	}
	key := t.Name + "-" + t.Version
	if c, ok := m.inflight[key]; ok {
		m.mu.Unlock()
		select {
		case <-c.done:
			return c.path, c.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	m.inflight[key] = c
	m.mu.Unlock()

	c.path, c.err = m.fetch(ctx, t)
	close(c.done)
	m.mu.Lock()
	delete(m.inflight, key)
	m.mu.Unlock()
	return c.path, c.err
}

func (m *Manager) fetch(ctx context.Context, t proto.Tool) (string, error) {
	want := strings.ToLower(strings.TrimPrefix(t.SHA256, "sha256:"))
	if len(want) != 64 {
		return "", fmt.Errorf("the tool %s %s has no valid SHA-256 checksum, so it cannot be verified", t.Name, t.Version)
	}
	dst := filepath.Join(m.dir, t.Name+"-"+t.Version)
	marker := dst + ".sha256"
	if b, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(b)) == want {
		if st, err := os.Stat(dst); err == nil && st.Mode().IsRegular() {
			return dst, nil
		}
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", fmt.Errorf("could not create the tools directory %s: %w", m.dir, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return "", fmt.Errorf("the download address of %s is not valid: %w", t.Name, err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download %s %s: %w", t.Name, t.Version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not download %s %s: the server answered %s", t.Name, t.Version, resp.Status)
	}
	tmp, err := os.CreateTemp(m.dir, "."+t.Name+"-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, MaxDownloadBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("could not download %s %s: %w", t.Name, t.Version, err)
	}
	if n > MaxDownloadBytes {
		return "", fmt.Errorf("the download of %s %s is larger than %d MB and was refused", t.Name, t.Version, MaxDownloadBytes>>20)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return "", fmt.Errorf("the download of %s %s has checksum %s but %s was expected; it was discarded", t.Name, t.Version, got, want)
	}
	bin := tmp.Name()
	if t.Path != "" {
		out, err := extract(tmp.Name(), t.Path, m.dir)
		if err != nil {
			return "", fmt.Errorf("could not unpack %s from the %s archive: %w", t.Path, t.Name, err)
		}
		defer os.Remove(out)
		bin = out
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(bin, dst); err != nil {
		return "", fmt.Errorf("could not install %s: %w", dst, err)
	}
	if err := os.WriteFile(marker, []byte(want+"\n"), 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// extract copies one regular file out of a .tar.gz (or plain .tar) into a
// temporary file in dir.
func extract(archive, member, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var r io.Reader = f
	var magic [2]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		r = gz
	}
	want := path.Clean(strings.TrimPrefix(member, "./"))
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("the archive has no file %s", member)
		}
		if err != nil {
			return "", err
		}
		if path.Clean(strings.TrimPrefix(h.Name, "./")) != want {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("%s in the archive is not a regular file", member)
		}
		out, err := os.CreateTemp(dir, ".extract-*")
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, io.LimitReader(tr, MaxDownloadBytes))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(out.Name())
			return "", err
		}
		return out.Name(), nil
	}
}
