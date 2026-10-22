// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"strings"
	"testing"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// TestRenderEveryTemplateAtHostSizes renders every service of every built-in
// template for 1, 4 and 32 GB hosts, with and without a backup bucket.
func TestRenderEveryTemplateAtHostSizes(t *testing.T) {
	for _, bucket := range []bool{false, true} {
		e := newEnv(t)
		if bucket {
			e.setBucket()
		}
		ids, err := e.d.cat.TemplateIDs()
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 16 {
			t.Fatalf("got %d templates, want 16 (4 databases and 12 apps)", len(ids))
		}
		for _, id := range ids {
			tm, err := e.d.cat.Template(id)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			// Add every service of the template so cross-service references resolve.
			svcs := map[string]string{}
			for _, s := range tm.Services {
				svcs[s.Name] = e.addTemplateService(id, s.Name, false)
			}
			for _, s := range tm.Services {
				snap, err := e.cfg.ServiceInstance(e.ctx(), e.envID, svcs[s.Name])
				if err != nil {
					t.Fatal(err)
				}
				var prev string
				for _, gb := range []int64{1, 4, 32} {
					host := proto.Facts{Arch: "amd64", CPUs: 4, MemBytes: gb << 30, DataDir: "/var/lib/5050"}
					r, err := e.d.Render(e.ctx(), snap, host)
					if err != nil {
						t.Fatalf("%s/%s at %d GB (bucket %v): %v", id, s.Name, gb, bucket, err)
					}
					if len(r.Files) != len(s.Files) {
						t.Fatalf("%s/%s: rendered %d files, want %d", id, s.Name, len(r.Files), len(s.Files))
					}
					var all strings.Builder
					for _, f := range r.Files {
						if strings.Contains(string(f.Content), "<no value>") {
							t.Fatalf("%s/%s: %s has an unset template value", id, s.Name, f.Path)
						}
						all.Write(f.Content)
					}
					if tm.Category == "database" && r.MemoryReservationMB == 0 {
						t.Fatalf("%s/%s: databases reserve memory", id, s.Name)
					}
					if s.Data == "postgres" {
						want := "off"
						if bucket {
							want = "on"
						}
						if r.Env["FIFTY_ARCHIVE"] != want {
							t.Fatalf("%s/%s: FIFTY_ARCHIVE=%q, want %q", id, s.Name, r.Env["FIFTY_ARCHIVE"], want)
						}
					}
					// Sized configs change with host memory.
					if gb > 1 && prev != "" && all.Len() > 0 && strings.Contains(prev, "MB") && prev == all.String() && s.Data == "postgres" {
						t.Fatalf("%s/%s: config does not scale with host memory", id, s.Name)
					}
					prev = all.String()
				}
			}
		}
	}
}
