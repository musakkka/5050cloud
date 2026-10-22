// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// railpackBinary returns a verified Railpack CLI of the pinned version.
// The current pin comes from agentapi.Tools when that serves the same
// version (the control plane may point it at a mirror); every other pin is
// downloaded once into <DataDir>/tools/railpack/<version> and checked
// against the SHA-256 in pins.go.
func (b *builder) railpackBinary(ctx context.Context, pin RailpackPin, log func(string)) (string, error) {
	if b.opts.Tools != nil && pin.Version == CurrentRailpack().Version {
		if p, err := b.opts.Tools.Path(ctx, "railpack"); err == nil && b.railpackVersionOf(ctx, p) == pin.Version {
			return p, nil
		}
	}
	dir := filepath.Join(b.opts.DataDir, "tools", "railpack", pin.Version)
	bin := filepath.Join(dir, "railpack")
	arch := runtime.GOARCH
	a, ok := pin.Archives[arch]
	if !ok {
		return "", fail("Railpack %s has no release for %s hosts.", pin.Version, arch)
	}
	marker := filepath.Join(dir, ".sha256")
	if got, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(got)) == a.SHA256 {
		if st, err := os.Stat(bin); err == nil && st.Mode().IsRegular() {
			return bin, nil
		}
	}
	log(fmt.Sprintf("Downloading Railpack %s", pin.Version))
	data, err := b.download(ctx, a.URL, 200<<20)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != a.SHA256 {
		return "", fail("The Railpack %s download did not match its pinned checksum, so it was not used. Check the network path to github.com.", pin.Version)
	}
	exe, err := extractRailpack(data)
	if err != nil {
		return "", fail("The Railpack %s archive could not be unpacked: %v.", pin.Version, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fail("The Railpack directory %s could not be created: %v.", dir, err)
	}
	if err := writeFileAtomic(bin, exe, 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(marker, []byte(a.SHA256+"\n"), 0o644); err != nil {
		return "", err
	}
	return bin, nil
}

// railpackVersionOf runs `railpack --version` ("railpack version 0.40.1").
func (b *builder) railpackVersionOf(ctx context.Context, bin string) string {
	var last string
	err := b.run.Run(ctx, cmdSpec{Path: bin, Args: []string{"--version"}, Env: []string{"PATH=" + defaultPath()}}, func(l string) { last = l })
	if err != nil {
		return ""
	}
	f := strings.Fields(last)
	if len(f) == 0 {
		return ""
	}
	return strings.TrimPrefix(f[len(f)-1], "v")
}

func (b *builder) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fail("The download URL %s is not valid: %v.", url, err)
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail("%s could not be downloaded: %v.", url, errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fail("%s could not be downloaded: the server answered %s.", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fail("%s could not be downloaded: %v.", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fail("%s is larger than expected and was not used.", url)
	}
	return data, nil
}

// extractRailpack returns the "railpack" file from a release tarball.
func extractRailpack(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive has no railpack binary")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "railpack" {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}
