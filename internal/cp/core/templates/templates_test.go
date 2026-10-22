// SPDX-License-Identifier: AGPL-3.0-only

package templates

import (
	"testing"

	builtin "github.com/musakkka/5050cloud/templates"
)

func TestLoadBuiltins(t *testing.T) {
	c, err := Load(builtin.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"postgres", "redis", "n8n", "uptime-kuma"} {
		tm, ok := c.Get(id)
		if !ok {
			t.Fatalf("template %s missing", id)
		}
		if len(Ordered(tm)) != len(tm.Services) {
			t.Fatalf("%s: order lost services", id)
		}
		if _, ok := c.Icon(id + ".svg"); !ok {
			t.Errorf("%s: no icon", id)
		}
	}
	n8n, _ := c.Get("n8n")
	ord := Ordered(n8n)
	if len(ord) > 1 && len(ord[0].DependsOn) != 0 {
		t.Fatalf("first n8n service has dependencies: %+v", ord[0].DependsOn)
	}
	if len(c.List("postgres")) == 0 {
		t.Fatal("search found nothing")
	}
}
