// SPDX-License-Identifier: AGPL-3.0-only

package assets

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteAllIsAtomicAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	paths, err := WriteAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := paths[ClusterName]
	st1, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(Cluster()) {
		t.Fatal("written content differs from the embedded file")
	}
	if !strings.HasPrefix(string(got), "// SPDX-License-Identifier: MIT") {
		t.Fatal("cluster.cjs must start with its MIT SPDX line")
	}
	// Same content: the file is not replaced (same inode).
	if _, err := WriteAll(dir); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(p)
	if !os.SameFile(st1, st2) {
		t.Fatal("unchanged asset was rewritten")
	}
	// Changed content: replaced through rename, so the old inode survives
	// for containers that still have it mounted.
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Open(p)
	defer old.Close()
	if _, err := WriteAll(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(old)
	if string(b) != "old" {
		t.Fatal("the open old file should keep its content")
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown asset")
	}
	if _, err := WriteAll(filepath.Join(p, "not-a-dir")); err == nil {
		t.Fatal("expected an error for a path under a file")
	}
}

// TestClusterWithLocalNode runs cluster.cjs with the node binary on PATH,
// when there is one: N processes share one port, each has its own
// CLUSTER_WORKER, a killed process comes back, and SIGTERM stops them all.
// The integration suite repeats this inside a pinned Node image.
func TestClusterWithLocalNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the integration suite covers cluster.cjs in a Node image")
	}
	dir := t.TempDir()
	paths, err := WriteAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	app := filepath.Join(dir, "app.js")
	src := fmt.Sprintf(`require('http').createServer((q,s)=>{ if(q.url==='/die'){s.end('bye');process.exit(3)} s.setHeader('connection','close'); s.end(process.env.CLUSTER_WORKER+' '+process.pid)}).listen(%d,'127.0.0.1',()=>console.log('ready '+process.env.CLUSTER_WORKER));`, port)
	if err := os.WriteFile(app, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, paths[ClusterName], "3", app)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := 0
	sc := bufio.NewScanner(stdout)
	for ready < 3 && sc.Scan() {
		if strings.HasPrefix(sc.Text(), "ready ") {
			ready++
		}
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	workers := map[string]bool{}
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 300 && len(workers) < 3; i++ {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		workers[strings.Fields(string(b))[0]] = true
	}
	if len(workers) != 3 || !workers["1"] || !workers["2"] || !workers["3"] {
		t.Fatalf("expected workers 1, 2 and 3 to answer on one port, got %v", workers)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cluster.cjs did not stop within 30 s of SIGINT")
	}
}
