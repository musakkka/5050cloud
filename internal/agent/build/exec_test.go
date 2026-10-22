// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerStreamsLines(t *testing.T) {
	var got []string
	err := execRunner{}.Run(context.Background(), cmdSpec{
		Path: "/bin/sh",
		Args: []string{"-c", `printf 'one\ntwo\r50%%\r100%%\n\n'; echo err >&2; echo "$SECRET_NAME"`},
		Env:  []string{"SECRET_NAME=from-env"},
	}, func(l string) { got = append(got, l) })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one", "two", "50%", "100%", "err", "from-env"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestExecRunnerEnvIsNotInherited(t *testing.T) {
	t.Setenv("FIFTY_TEST_LEAK", "leaked")
	var got []string
	err := execRunner{}.Run(context.Background(), cmdSpec{Path: "/bin/sh", Args: []string{"-c", `echo "[$FIFTY_TEST_LEAK]"`}}, func(l string) { got = append(got, l) })
	if err != nil || len(got) != 1 || got[0] != "[]" {
		t.Errorf("got %q err %v", got, err)
	}
}

func TestExecRunnerExitError(t *testing.T) {
	err := execRunner{}.Run(context.Background(), cmdSpec{Path: "/bin/sh", Args: []string{"-c", "echo last words; exit 3"}}, nil)
	var ee *exitError
	if !errors.As(err, &ee) || ee.Code != 3 || ee.Last != "last words" {
		t.Fatalf("err = %#v", err)
	}
	if ee.Error() != "sh exited with code 3: last words" {
		t.Errorf("Error() = %q", ee.Error())
	}
	err = execRunner{}.Run(context.Background(), cmdSpec{Path: "/does/not/exist"}, nil)
	var bf *buildFailure
	if !errors.As(err, &bf) || !strings.HasPrefix(bf.sentence, "The exist command could not be started on this host") {
		t.Errorf("err = %v", err)
	}
}

// Cancelling kills the whole process group, children included, quickly.
func TestExecRunnerCancelKillsGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// The child ignores SIGTERM, so only the group kill stops it.
		done <- execRunner{}.Run(ctx, cmdSpec{Path: "/bin/sh", Args: []string{"-c",
			`sh -c 'trap "" TERM; echo $$ > ` + pidFile + `; while true; do sleep 1; done' & wait`}}, nil)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t0 := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the command within 5 s")
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("stop took %s", d)
	}
	data, _ := os.ReadFile(pidFile)
	pid := strings.TrimSpace(string(data))
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat("/proc/" + pid); err == nil {
		t.Errorf("child %s survived the cancel", pid)
	}
}

func TestExecRunnerPreCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (execRunner{}).Run(ctx, cmdSpec{Path: "/bin/true"}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}
