// SPDX-License-Identifier: MIT

package cli

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaxUpload is the largest archive `5050 up` sends (SPEC section 12).
const MaxUpload = 500 << 20

var errTooLarge = errors.New("the archive is larger than 500 MB. Add large files to .5050ignore and try again")

// PackOptions controls Pack.
type PackOptions struct {
	// Root is the archive root directory.
	Root string
	// Only limits the archive to this subdirectory of Root (relative, "" = all).
	Only string
	// NoGitignore skips .gitignore files (.5050ignore still applies).
	NoGitignore bool
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > MaxUpload {
		return 0, errTooLarge
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Pack writes a gzip tar of the directory to w and returns the archived
// paths in order and the compressed size.
func Pack(o PackOptions, w io.Writer) ([]string, int64, error) {
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, 0, err
	}
	only := filepath.ToSlash(filepath.Clean(o.Only))
	if only == "." {
		only = ""
	}
	cw := &countWriter{w: w}
	gz := gzip.NewWriter(cw)
	tw := tar.NewWriter(gz)
	var ign ignoreSet
	var names []string

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		if d.IsDir() {
			if d.Name() == ".git" && rel != "" {
				return filepath.SkipDir
			}
			if rel != "" && ign.ignored(rel, true) {
				return filepath.SkipDir
			}
			if only != "" && rel != "" && !strings.HasPrefix(only+"/", rel+"/") && !strings.HasPrefix(rel, only+"/") {
				return filepath.SkipDir
			}
			if !o.NoGitignore {
				if err := ign.load(filepath.Join(p, ".gitignore"), rel); err != nil {
					return err
				}
			}
			if err := ign.load(filepath.Join(p, ".5050ignore"), rel); err != nil {
				return err
			}
			if rel == "" || (only != "" && !strings.HasPrefix(rel+"/", only+"/")) {
				return nil
			}
		} else if ign.ignored(rel, false) || (only != "" && !strings.HasPrefix(rel, only+"/")) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // sockets, devices, pipes
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = rel
		if info.IsDir() {
			h.Name += "/"
		}
		h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
		h.Format = tar.FormatPAX
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		names = append(names, h.Name)
		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return nil, cw.n, errTooLarge
		}
		return nil, cw.n, fmt.Errorf("could not pack %s: %w", root, err)
	}
	return names, cw.n, nil
}

// humanBytes prints 3.2 MB style sizes.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
