// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

func TestShellCEntrypoint(t *testing.T) {
	if !ShellCEntrypoint([]string{"/bin/bash", "-c"}) || !ShellCEntrypoint([]string{"sh", "-c"}) {
		t.Error("shell -c entrypoints not detected")
	}
	if ShellCEntrypoint([]string{"docker-entrypoint.sh"}) || ShellCEntrypoint(nil) {
		t.Error("a plain entrypoint was taken for a shell")
	}
}

func TestShellJoin(t *testing.T) {
	if got := ShellJoin([]string{"node", "server.js"}); got != "node server.js" {
		t.Errorf("got %q", got)
	}
	if got := ShellJoin([]string{"echo", "it's here"}); got != `echo 'it'\''s here'` {
		t.Errorf("got %q", got)
	}
}
