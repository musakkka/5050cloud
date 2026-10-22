// SPDX-License-Identifier: AGPL-3.0-only

package events

import (
	"testing"

	"github.com/musakkka/5050cloud/pkg/api"
)

func TestRouting(t *testing.T) {
	b := New()
	a := b.Subscribe("env_a", "usr_1", 4)
	other := b.Subscribe("env_b", "usr_2", 4)
	defer other.Close()
	b.Publish(api.Event{Type: "deployment", EnvironmentID: "env_a"})
	b.Publish(api.Event{Type: "host"})
	b.Publish(api.Event{Type: "changes", EnvironmentID: "env_b"})
	b.PublishUser("usr_1", api.Event{Type: "notification"})
	var got []string
	for len(a.C) > 0 {
		got = append(got, (<-a.C).Type)
	}
	if len(got) != 3 || got[0] != "deployment" || got[1] != "host" || got[2] != "notification" {
		t.Fatalf("env_a got %v", got)
	}
	if len(other.C) != 2 {
		t.Fatalf("env_b got %d", len(other.C))
	}
	if b.Count() != 2 {
		t.Fatal("count")
	}
	a.Close()
	a.Close()
	if b.Count() != 1 {
		t.Fatal("close")
	}
}

func TestDrop(t *testing.T) {
	b := New()
	s := b.Subscribe("e", "u", 0)
	for i := 0; i < 300; i++ {
		b.Publish(api.Event{Type: "x", EnvironmentID: "e"})
	}
	if s.Dropped() != 44 || s.Dropped() != 0 {
		t.Fatal("dropped count")
	}
}
