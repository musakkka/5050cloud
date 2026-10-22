// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/musakkka/5050cloud/internal/upgrade/minisign"
)

// Release is one verified release tarball for one architecture.
type Release struct {
	Version string
	URL     string // tarball
	SHA256  string // verified against the signed checksums file
}

var versionRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// ValidVersion reports whether v is a release tag such as v0.1.0 or
// v0.1.0-e2e.2.
func ValidVersion(v string) bool { return versionRE.MatchString(v) }

// ServerTarball is the name of the server tarball of a release
// (5050_0.1.0_linux_amd64.tar.gz for v0.1.0).
func ServerTarball(version, arch string) string {
	return fmt.Sprintf("5050_%s_linux_%s.tar.gz", strings.TrimPrefix(version, "v"), arch)
}

// Resolve finds a release (version "" = latest) and verifies its signed checksum.
func Resolve(ctx context.Context, o Options, version string) (Release, error) {
	o = o.Defaults()
	keys, err := o.keys()
	if err != nil {
		return Release{}, err
	}
	if version == "" || version == "latest" {
		b, err := fetch(ctx, o.HTTP, o.LatestURL, 256)
		if err != nil {
			return Release{}, fmt.Errorf("cannot find the latest release: %w", err)
		}
		version = strings.TrimSpace(string(b))
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !ValidVersion(version) {
		return Release{}, fmt.Errorf("%q is not a release version; use the form v0.1.0", version)
	}
	base := o.ReleaseBase + "/" + version + "/"
	sums, err := fetch(ctx, o.HTTP, base+"checksums.txt", 1<<20)
	if err != nil {
		return Release{}, fmt.Errorf("cannot download the checksums of %s: %w", version, err)
	}
	sig, err := fetch(ctx, o.HTTP, base+"checksums.txt.minisig", 64<<10)
	if err != nil {
		return Release{}, fmt.Errorf("cannot download the signature of %s: %w", version, err)
	}
	if _, _, err := minisign.Verify(keys, sums, sig); err != nil {
		return Release{}, fmt.Errorf("the checksums of %s failed signature verification, so nothing was installed: %w", version, err)
	}
	name := ServerTarball(version, o.Arch)
	sum, err := lookupChecksum(sums, name)
	if err != nil {
		return Release{}, fmt.Errorf("release %s: %w", version, err)
	}
	return Release{Version: version, URL: base + name, SHA256: sum}, nil
}

// lookupChecksum finds name in a checksums.txt ("<sha256>  <file>" lines).
func lookupChecksum(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		if strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		if b, err := hex.DecodeString(f[0]); err != nil || len(b) != 32 {
			return "", fmt.Errorf("the checksum of %s is not a SHA-256 digest", name)
		}
		return strings.ToLower(f[0]), nil
	}
	return "", fmt.Errorf("there is no %s in its checksums, so this release has no build for this architecture", name)
}

// fetch GETs url and returns at most limit bytes.
func fetch(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New(url + " is larger than expected")
	}
	return b, nil
}
