// SPDX-License-Identifier: AGPL-3.0-only

package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
)

// swapLink points link at target atomically (a new link, then a rename over
// the old one) and returns the previous target ("" when there was none).
func swapLink(link, target string) (string, error) {
	prev, err := os.Readlink(link)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot read the link %s: %w", link, err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return "", err
	}
	tmp := fmt.Sprintf("%s.new-%d", link, os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return "", fmt.Errorf("cannot create a link to %s: %w", target, err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("cannot switch %s to %s: %w", link, target, err)
	}
	return prev, nil
}

// linkTarget resolves link to an absolute directory.
func linkTarget(link string) (string, error) {
	t, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("cannot read the link %s: %w", link, err)
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(link), t)
	}
	return filepath.Clean(t), nil
}
