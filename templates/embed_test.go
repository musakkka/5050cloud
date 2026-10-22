// SPDX-License-Identifier: MIT

package templates

import (
	"io/fs"
	"os"
	"testing"
)

// TestEverythingEmbedded fails when a directory is added to templates/ but
// not to the go:embed lines of embed.go.
func TestEverythingEmbedded(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(FS, e.Name()); err != nil {
			t.Errorf("templates/%s is not embedded: add it to the go:embed lines in embed.go", e.Name())
			continue
		}
		if e.Name()[0] == '_' {
			continue
		}
		if _, err := fs.Stat(FS, e.Name()+"/template.yaml"); err != nil {
			t.Errorf("templates/%s has no template.yaml", e.Name())
		}
	}
	for _, f := range []string{"tools.json", "images.json"} {
		if _, err := fs.Stat(FS, f); err != nil {
			t.Errorf("%s is not embedded", f)
		}
	}
}
