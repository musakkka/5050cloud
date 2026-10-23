// SPDX-License-Identifier: MIT

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func listing(t *testing.T, b []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, h.Name)
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

var packTree = map[string]string{
	".gitignore":             "node_modules/\n*.log\n/dist\n!keep.log\nsecret?.txt\n**/cache/**\n",
	".5050ignore":            "big.bin\n",
	".git/HEAD":              "ref: refs/heads/main\n",
	"package.json":           "{}",
	"server.js":              "",
	"debug.log":              "",
	"keep.log":               "",
	"big.bin":                "",
	"secret1.txt":            "",
	"secret10.txt":           "",
	"dist/out.js":            "",
	"src/dist/kept.js":       "",
	"src/index.js":           "",
	"src/.gitignore":         "local.json\n",
	"src/local.json":         "",
	"local.json":             "",
	"node_modules/x/i.js":    "",
	"src/cache/a/b.txt":      "",
	"docs/readme.md":         "",
	"docs/node_modules/y.js": "",
}

func TestPackGolden(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, packTree)
	var out strings.Builder
	for _, tc := range []struct {
		name string
		o    PackOptions
	}{
		{"default", PackOptions{Root: root}},
		{"no-gitignore", PackOptions{Root: root, NoGitignore: true}},
		{"only src", PackOptions{Root: root, Only: "src"}},
	} {
		var buf bytes.Buffer
		names, size, err := Pack(tc.o, &buf)
		if err != nil {
			t.Fatal(err)
		}
		if size != int64(buf.Len()) {
			t.Errorf("%s: size %d, wrote %d", tc.name, size, buf.Len())
		}
		got := listing(t, buf.Bytes())
		if strings.Join(got, "\n") != strings.Join(names, "\n") {
			t.Errorf("%s: names and archive differ", tc.name)
		}
		out.WriteString("## " + tc.name + "\n" + strings.Join(got, "\n") + "\n")
	}
	golden(t, "up.golden", out.String())
}

func TestArchiveRoot(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"api/main.go": "", "web/x": ""})
	r, only, err := archiveRoot(root, filepath.Join(root, "api"), nil, false)
	if err != nil || r != root || only != "" {
		t.Errorf("no path: %q %q %v", r, only, err)
	}
	r, only, err = archiveRoot(root, root, []string{"api"}, false)
	if err != nil || r != root || only != "api" {
		t.Errorf("path: %q %q %v", r, only, err)
	}
	r, only, err = archiveRoot(root, root, []string{"api"}, true)
	if err != nil || r != filepath.Join(root, "api") || only != "" {
		t.Errorf("path-as-root: %q %q %v", r, only, err)
	}
}

func TestPackTooLarge(t *testing.T) {
	cw := &countWriter{w: io.Discard, n: MaxUpload - 1}
	if _, err := cw.Write([]byte("ab")); !errors.Is(err, errTooLarge) {
		t.Fatalf("got %v", err)
	}
}
