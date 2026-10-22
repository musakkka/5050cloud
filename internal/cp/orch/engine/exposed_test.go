// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

func TestOnlyTCPPort(t *testing.T) {
	cases := []struct {
		in   map[string]struct{}
		want int
	}{
		{nil, 0},
		{map[string]struct{}{"80/tcp": {}}, 80},
		{map[string]struct{}{"53/udp": {}, "8080/tcp": {}}, 8080},
		{map[string]struct{}{"80/tcp": {}, "443/tcp": {}}, 0},
		{map[string]struct{}{"3000": {}}, 3000},
	}
	for _, c := range cases {
		if got := onlyTCPPort(c.in); got != c.want {
			t.Errorf("onlyTCPPort(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestStartArgvKeepsEqualsInArguments(t *testing.T) {
	got := startArgv("postgres -c config_file=/etc/postgresql/5050.conf")
	if len(got) != 3 || got[0] != "postgres" || got[2] != "config_file=/etc/postgresql/5050.conf" {
		t.Errorf("startArgv = %q", got)
	}
	if got := startArgv("NODE_ENV=production node server.js"); len(got) != 3 || got[0] != "sh" {
		t.Errorf("a leading assignment needs a shell, got %q", got)
	}
}
