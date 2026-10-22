// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Source fetching (docs/SPEC.md 8.2 step 1).

const (
	// maxArchiveBytes is the upload limit for `5050 up` tarballs.
	maxArchiveBytes = 500 << 20
	// maxExtractedBytes bounds what an archive may expand to.
	maxExtractedBytes = 4 << 30
	// maxArchiveEntries bounds the number of files in an archive.
	maxArchiveEntries = 200000
	// maxSymlinkHops bounds symlink resolution inside a checkout.
	maxSymlinkHops = 64
)

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// fetchGit checks out exactly src.Commit into dir. The token travels in the
// child environment as an http.extraHeader (GIT_CONFIG_COUNT), never in the
// URL, argv or the log.
func (b *builder) fetchGit(ctx context.Context, src *proto.GitSource, dir string, logf func(string)) error {
	u, err := url.Parse(src.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fail("The repository URL %s is not a valid https URL.", redactURL(src.URL))
	}
	if u.User != nil {
		return fail("The repository URL must not contain credentials. Connect the repository through the GitHub App instead.")
	}
	if !shaRe.MatchString(src.Commit) {
		return fail("The commit %q is not a valid commit id.", src.Commit)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("The build directory could not be created: %v.", err)
	}
	home := filepath.Join(filepath.Dir(dir), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fail("The build directory could not be created: %v.", err)
	}
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ASKPASS=/bin/false",
		"LC_ALL=C",
	}
	if src.Token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + src.Token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
		)
	}
	git := func(args ...string) error {
		return b.run.Run(ctx, cmdSpec{Path: b.gitPath, Args: append([]string{"-C", dir}, args...), Env: env}, logf)
	}
	short := src.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	logf(fmt.Sprintf("Fetching %s at %s", redactURL(src.URL), short))
	if err := git("init", "-q"); err != nil {
		return fail("git init failed: %v.", err)
	}
	if err := git("remote", "add", "origin", src.URL); err != nil {
		return fail("git remote add failed: %v.", err)
	}
	fetchErr := git("-c", "protocol.version=2", "fetch", "--quiet", "--no-tags", "--depth", "1", "origin", src.Commit)
	if fetchErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if src.Branch == "" {
			return fail("Commit %s could not be fetched from %s. Check that the repository is reachable and the commit exists.", short, redactURL(src.URL))
		}
		// Some servers refuse fetching a commit by id. Fall back to the
		// branch tip with some history and look for the commit there.
		logf("The server did not allow fetching the commit directly; fetching the branch " + src.Branch + " instead.")
		if err := git("fetch", "--quiet", "--no-tags", "--depth", "50", "origin", "refs/heads/"+src.Branch); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fail("Branch %s could not be fetched from %s. Check that the repository is reachable.", src.Branch, redactURL(src.URL))
		}
		if err := git("cat-file", "-e", src.Commit+"^{commit}"); err != nil {
			return fail("Commit %s is not among the last 50 commits of %s. It may have been force-pushed away; deploy a newer commit.", short, src.Branch)
		}
	}
	if err := git("-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", src.Commit); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail("Commit %s could not be checked out: %v.", short, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitmodules")); err == nil {
		logf("Fetching git submodules")
		if err := git("submodule", "update", "--init", "--recursive", "--depth", "1"); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fail("The git submodules could not be fetched. Check that every submodule URL is reachable with the same access as the repository.")
		}
	}
	// The .git directory is not part of the build context.
	return os.RemoveAll(filepath.Join(dir, ".git"))
}

// redactURL drops credentials and query strings from a URL for the log.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "the repository"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// fetchTarball downloads an upload with the host token and extracts it.
func (b *builder) fetchTarball(ctx context.Context, rawURL, dir string, logf func(string)) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fail("The upload URL is not a valid https URL.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fail("The upload URL is not valid: %v.", err)
	}
	if b.opts.HostToken != nil {
		if tok := b.opts.HostToken(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	logf("Downloading the uploaded source")
	resp, err := b.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail("The uploaded source could not be downloaded: %v.", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail("The uploaded source could not be downloaded: the control plane answered %s.", resp.Status)
	}
	if resp.ContentLength > maxArchiveBytes {
		return errArchiveTooLarge
	}
	counted := &countingReader{r: resp.Body, limit: maxArchiveBytes}
	if err := extractArchive(counted, dir); err != nil {
		if errors.Is(err, errReadLimit) {
			return errArchiveTooLarge
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	logf(fmt.Sprintf("Extracted %s of source", humanBytes(counted.n)))
	return nil
}

var (
	errArchiveTooLarge = &buildFailure{sentence: "The uploaded archive is larger than 500 MB. Exclude large files with .gitignore or .railwayignore and upload again."}
	errReadLimit       = errors.New("read limit reached")
)

type countingReader struct {
	r     io.Reader
	n     int64
	limit int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.limit {
		return n, errReadLimit
	}
	return n, err
}

// extractArchive unpacks a tar or tar.gz stream into dir. It rejects
// absolute paths, paths with "..", entries written through symlinks, links
// that point outside dir, and device files.
func extractArchive(r io.Reader, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("The build directory could not be created: %v.", err)
	}
	br := bufio.NewReader(r)
	var src io.Reader = br
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return fail("The uploaded archive is not a valid gzip file: %v.", err)
		}
		defer func() { _ = gz.Close() }()
		src = gz
	}
	tr := tar.NewReader(src)
	var total int64
	var links []string
	entries := 0
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errReadLimit) {
				return err
			}
			return fail("The uploaded archive is not a valid tar file: %v.", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return fail("The uploaded archive has more than %d files. Exclude generated files and upload again.", maxArchiveEntries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeXHeader {
			continue
		}
		rel, err := safeArchivePath(h.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue // the archive root itself
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := noSymlinkParents(dir, rel); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fail("The archive entry %s could not be extracted: %v.", rel, err)
			}
		case tar.TypeReg, typeRegA:
			if err := writeFile(tr, target, h, &total); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := checkLinkTarget(dir, rel, h.Linkname); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return fail("The archive entry %s could not be extracted: %v.", rel, err)
			}
			links = append(links, rel)
		case tar.TypeLink:
			lrel, err := safeArchivePath(h.Linkname)
			if err != nil || lrel == "" {
				return fail("The uploaded archive contains a hard link %s that points outside the project.", rel)
			}
			if err := noSymlinkParents(dir, lrel); err != nil {
				return err
			}
			src := filepath.Join(dir, filepath.FromSlash(lrel))
			st, err := os.Lstat(src)
			if err != nil || !st.Mode().IsRegular() {
				return fail("The uploaded archive contains a hard link %s to %s, which is not a file earlier in the archive.", rel, lrel)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fail("The archive entry %s could not be extracted: %v.", rel, err)
			}
			_ = os.Remove(target)
			if err := os.Link(src, target); err != nil {
				return fail("The archive entry %s could not be extracted: %v.", rel, err)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return fail("The uploaded archive contains a device or pipe %s. Only files, directories and links are allowed.", rel)
		default:
			// Other types (sparse files, vendor extensions) are skipped.
		}
	}
	// A later entry can change what an earlier link resolves to, so check
	// every link again against the finished tree.
	for _, rel := range links {
		if err := resolveInside(dir, rel); err != nil {
			return fail("The uploaded archive contains a symlink %s that points outside the project.", rel)
		}
	}
	return nil
}

func writeFile(r io.Reader, target string, h *tar.Header, total *int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fail("The archive entry %s could not be extracted: %v.", h.Name, err)
	}
	_ = os.Remove(target) // never write through an existing link
	mode := os.FileMode(0o644)
	if h.FileInfo().Mode()&0o111 != 0 {
		mode = 0o755
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fail("The archive entry %s could not be extracted: %v.", h.Name, err)
	}
	defer f.Close()
	remaining := maxExtractedBytes - *total
	n, err := io.Copy(f, io.LimitReader(r, remaining+1))
	*total += n
	if err != nil {
		if errors.Is(err, errReadLimit) {
			return err
		}
		return fail("The archive entry %s could not be extracted: %v.", h.Name, err)
	}
	if *total > maxExtractedBytes {
		return fail("The uploaded archive expands to more than %s. Exclude large files and upload again.", humanBytes(maxExtractedBytes))
	}
	return nil
}

// safeArchivePath cleans an entry name. It rejects absolute paths, drive
// letters and any ".." element; "" is the archive root.
func safeArchivePath(name string) (string, error) {
	n := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(n, "/") || (len(n) >= 2 && n[1] == ':') || strings.ContainsRune(n, 0) {
		return "", fail("The uploaded archive contains an absolute path %q. Archives may only contain paths inside the project.", name)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fail("The uploaded archive contains the unsafe path %q. Archives may only contain paths inside the project.", name)
		}
	}
	c := path.Clean(n)
	if c == "." {
		return "", nil
	}
	return c, nil
}

// noSymlinkParents refuses an entry whose parent directories include a
// symlink, so nothing is ever written through a link.
func noSymlinkParents(root, rel string) error {
	parts := strings.Split(rel, "/")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		st, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fail("The archive entry %s could not be extracted: %v.", rel, err)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fail("The uploaded archive writes %s through a symlink. Archives may only contain paths inside the project.", rel)
		}
	}
	return nil
}

// checkLinkTarget refuses absolute link targets and targets that leave
// root, resolving links already extracted.
func checkLinkTarget(root, rel, target string) error {
	if target == "" || strings.HasPrefix(target, "/") || strings.ContainsRune(target, 0) || (len(target) >= 2 && target[1] == ':') {
		return fail("The uploaded archive contains a symlink %s with an absolute target. Use relative links inside the project.", rel)
	}
	joined := path.Join(path.Dir(rel), target)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return fail("The uploaded archive contains a symlink %s that points outside the project.", rel)
	}
	if err := resolveInside(root, path.Join(path.Dir(rel), target)); err != nil {
		return fail("The uploaded archive contains a symlink %s that points outside the project.", rel)
	}
	return nil
}

// typeRegA is the pre-POSIX regular file flag ('\x00') old tar writers
// still emit; archive/tar deprecates the named constant.
const typeRegA = '\x00'

var errOutside = errors.New("path leaves the root")

// resolveInside follows rel inside root component by component, resolving
// symlinks the way the kernel would, and fails when any step leaves root.
// Missing components are fine (a dangling link inside root is harmless).
func resolveInside(root, rel string) error {
	pending := splitClean(rel)
	var cur []string
	hops := 0
	for len(pending) > 0 {
		p := pending[0]
		pending = pending[1:]
		switch p {
		case "", ".":
			continue
		case "..":
			if len(cur) == 0 {
				return errOutside
			}
			cur = cur[:len(cur)-1]
			continue
		}
		next := append(append([]string{}, cur...), p)
		full := filepath.Join(append([]string{root}, next...)...)
		st, err := os.Lstat(full)
		if err != nil {
			cur = next
			continue
		}
		if st.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		hops++
		if hops > maxSymlinkHops {
			return errOutside
		}
		target, err := os.Readlink(full)
		if err != nil {
			return errOutside
		}
		if strings.HasPrefix(target, "/") {
			return errOutside
		}
		pending = append(splitClean(target), pending...)
	}
	return nil
}

func splitClean(p string) []string {
	return strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
}

// insideRoot reports whether rel (a clean slash path) stays inside root.
func insideRoot(root, rel string) error {
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, "/") {
		return errOutside
	}
	return resolveInside(root, rel)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
