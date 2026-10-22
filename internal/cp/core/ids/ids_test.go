// SPDX-License-Identifier: AGPL-3.0-only

package ids

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestNewShape(t *testing.T) {
	re := regexp.MustCompile(`^svc_[0-9a-hjkmnp-tv-z]{26}$`)
	for i := 0; i < 100; i++ {
		id := New(Service)
		if !re.MatchString(id) {
			t.Fatalf("id %q does not match the ULID shape", id)
		}
		if !Has(id, Service) || Has(id, Project) {
			t.Fatalf("Has is wrong for %q", id)
		}
	}
}

func TestMonotonic(t *testing.T) {
	now := time.Now()
	var got []string
	for i := 0; i < 1000; i++ {
		got = append(got, ULID(now))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatal("ids made in one millisecond are not sorted")
	}
	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			t.Fatalf("duplicate id %s", g)
		}
		seen[g] = true
	}
	later := ULID(now.Add(time.Second))
	if later <= got[len(got)-1] {
		t.Fatal("a later time sorts before an earlier one")
	}
}

func TestRandom(t *testing.T) {
	s := Random(40, Alnum)
	if len(s) != 40 {
		t.Fatalf("length %d", len(s))
	}
	for _, c := range s {
		if !strings.ContainsRune(Alnum, c) {
			t.Fatalf("unexpected character %q", c)
		}
	}
	if Random(0, Alnum) != "" || Random(3, "") != "" {
		t.Fatal("empty cases must return an empty string")
	}
	for i := 0; i < 200; i++ {
		v := RandomInt(1000, 1002)
		if v < 1000 || v > 1002 {
			t.Fatalf("RandomInt out of range: %d", v)
		}
	}
	if RandomInt(5, 5) != 5 || RandomInt(7, 3) != 7 {
		t.Fatal("degenerate ranges must return min")
	}
	if !regexp.MustCompile(`^[a-z]+-[a-z]+$`).MatchString(TwoWordName()) {
		t.Fatal("two word name has the wrong shape")
	}
}
