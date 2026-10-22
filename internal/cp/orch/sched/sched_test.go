// SPDX-License-Identifier: AGPL-3.0-only

package sched

import (
	"errors"
	"testing"
)

func host(id string, mem int, roles ...string) Host {
	return Host{ID: id, Name: id, Roles: roles, State: StateOnline, Arch: "amd64", CPUs: 4, MemMB: mem}
}

func TestPlaceSpreadsAndRespectsRoles(t *testing.T) {
	hosts := []Host{host("a", 8192, "app"), host("b", 8192, "app"), host("c", 8192, "data")}
	counts := map[string]int{"a": 1}
	got, err := Place(hosts, counts, Request{EnvID: "e", ServiceID: "s", ReservationMB: 256})
	if err != nil || got != "b" {
		t.Fatalf("place = %q, %v; want b", got, err)
	}
	if _, err := Place(hosts, nil, Request{EnvID: "e", ServiceID: "s", Roles: []string{"build"}}); err == nil {
		t.Fatal("no host carries build; want an error")
	} else {
		var ne *NoEligibleError
		if !errors.As(err, &ne) {
			t.Fatalf("want NoEligibleError, got %T %v", err, err)
		}
	}
}

func TestPlaceNoCapacity(t *testing.T) {
	hosts := []Host{host("a", 1024, "app")}
	_, err := Place(hosts, nil, Request{EnvID: "e", ServiceID: "s", ReservationMB: 64 * 1024})
	var nc *NoCapacityError
	if !errors.As(err, &nc) {
		t.Fatalf("want NoCapacityError, got %T %v", err, err)
	}
}

func TestOfflineHostNotSchedulable(t *testing.T) {
	h := host("a", 8192, "app")
	h.State = StateOffline
	if h.Schedulable() {
		t.Fatal("offline host must not take replicas")
	}
}
