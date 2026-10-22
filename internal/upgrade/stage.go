// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

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
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxTarball   = 512 << 20 // a release tarball is about 60 MB
	maxTarMember = 256 << 20
	shaFile      = ".sha256"
)

// Stage downloads r, checks its SHA-256 and unpacks it into
// <InstallDir>/releases/<version>, returning that directory. A directory
// that already holds the same verified tarball is reused. want names the
// files that must be in the tarball.
func Stage(ctx context.Context, o Options, r Release, want ...string) (string, error) {
	o = o.Defaults()
	if !ValidVersion(r.Version) {
		return "", fmt.Errorf("%q is not a release version", r.Version)
	}
	dir := o.releaseDir(r.Version)
	if b, err := os.ReadFile(filepath.Join(dir, shaFile)); err == nil && strings.TrimSpace(string(b)) == r.SHA256 {
		if err := hasFiles(dir, want); err == nil {
			return dir, nil
		}
	}
	if err := os.MkdirAll(o.releasesDir(), 0o755); err != nil {
		return "", err
	}
	tmpFile, err := os.CreateTemp(o.releasesDir(), ".download-*.tar.gz")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()
	if err := download(ctx, o.HTTP, r.URL, tmpFile, r.SHA256); err != nil {
		return "", err
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp(o.releasesDir(), ".unpack-"+r.Version+"-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	if err := untar(tmpFile, tmpDir); err != nil {
		return "", fmt.Errorf("cannot unpack %s: %w", r.URL, err)
	}
	if err := hasFiles(tmpDir, want); err != nil {
		return "", fmt.Errorf("the release %s is incomplete: %w", r.Version, err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, shaFile), []byte(r.SHA256+"\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpDir, 0o755); err != nil {
		return "", err
	}
	// A directory of the same version without a matching checksum (an
	// interrupted earlier attempt) is replaced, unless something runs from it.
	if _, err := os.Stat(dir); err == nil {
		if inUse(o, dir) {
			return "", fmt.Errorf("%s is in use and does not match the release checksum; remove it by hand after checking what runs from it", dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmpDir, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func inUse(o Options, dir string) bool {
	for _, l := range []string{o.CurrentLink(), o.AgentLink()} {
		if t, err := linkTarget(l); err == nil && t == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func hasFiles(dir string, want []string) error {
	for _, w := range want {
		st, err := os.Stat(filepath.Join(dir, w))
		if err != nil {
			return fmt.Errorf("%s is missing", w)
		}
		if st.Mode()&0o111 == 0 {
			return fmt.Errorf("%s is not executable", w)
		}
	}
	return nil
}

func download(ctx context.Context, c *http.Client, url string, w io.Writer, wantSHA string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("cannot download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot download %s: the server answered %s", url, resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, maxTarball+1))
	if err != nil {
		return fmt.Errorf("the download of %s stopped: %w", url, err)
	}
	if n > maxTarball {
		return fmt.Errorf("%s is larger than any release tarball should be", url)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA) {
		return fmt.Errorf("the SHA-256 of %s is %s, but the signed checksum says %s, so the file was not used", url, got, wantSHA)
	}
	return nil
}

// untar unpacks a .tar.gz into dir. Only regular files and directories are
// written; absolute paths, "..", links and devices are refused.
func untar(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := safePath(h.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		dst := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size > maxTarMember {
				return fmt.Errorf("%s is too large", h.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxTarMember)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s is a link or special file, which a release tarball never contains", h.Name)
		}
	}
}

func safePath(name string) (string, error) {
	n := filepath.ToSlash(name)
	if strings.HasPrefix(n, "/") {
		return "", fmt.Errorf("%s is an absolute path", name)
	}
	clean := filepath.Clean(n)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s points outside the release directory", name)
	}
	return clean, nil
}

// Prune removes release directories other than the ones the links point at
// and the newest keep others.
func Prune(o Options, keep int) error {
	o = o.Defaults()
	entries, err := os.ReadDir(o.releasesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	type rel struct {
		path string
		mod  int64
	}
	var others []rel
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(o.releasesDir(), e.Name())
		if inUse(o, p) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		others = append(others, rel{p, info.ModTime().UnixNano()})
	}
	sort.Slice(others, func(i, j int) bool { return others[i].mod > others[j].mod })
	var firstErr error
	for i, r := range others {
		if i < keep {
			continue
		}
		if err := os.RemoveAll(r.path); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
