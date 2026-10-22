// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// golden compares got with testdata/<name>, or rewrites it with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run go test -run %s -update to create it)", path, err, t.Name())
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the rendered output (run with -update after checking the change)\n--- got ---\n%s", path, got)
	}
}
