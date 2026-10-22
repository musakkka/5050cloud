// SPDX-License-Identifier: MIT

package proto

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var camel = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// TestJSONTags fails when an exported field has no json tag or a tag that
// is not camelCase. LogRecord keeps its short on-disk names (svc, dep, rep).
func TestJSONTags(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "proto.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				continue // embedded
			}
			if field.Tag == nil {
				t.Errorf("%s.%s has no json tag", ts.Name.Name, field.Names[0].Name)
				continue
			}
			tag, _ := strconv.Unquote(field.Tag.Value)
			name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			if !camel.MatchString(name) {
				t.Errorf("%s.%s: json name %q is not camelCase", ts.Name.Name, field.Names[0].Name, name)
			}
		}
		return false
	})
}

func TestMessageTypesUnique(t *testing.T) {
	types := []string{THello, THelloAck, TDesired, TStateApplied, TReplicaStatus, TMetrics, TEvent,
		TBuild, TBuildLog, TBuildResult, TBuildCancel, TJob, TJobLog, TJobResult, TJobCancel,
		TLogsQuery, TLogsResult, TLogsSubscribe, TLogsUnsubscribe, TLogLines, THostLogsTail,
		TExecOpen, TExecResize, TExecClose, TExecExit, TTCPOpen, TTCPClose, TUpgrade,
		TCertIssued, TTokenRotate, TMaintenance, TAck}
	seen := map[string]bool{}
	for _, m := range types {
		if seen[m] {
			t.Errorf("message type %q is used twice", m)
		}
		seen[m] = true
	}
}

// TestDesiredStateRoundTrip checks the fields M0 added survive JSON.
func TestDesiredStateRoundTrip(t *testing.T) {
	in := DesiredState{
		Generation: 7, Epoch: "ep2", Adopt: true,
		Host: HostConfig{RegistryPull: Registry{Address: "127.0.0.1:5000"}, RegistryPush: &Registry{Address: "127.0.0.1:5000", Username: "push"}},
		Ingress: &Ingress{ACMEPrimary: true, StreamCloseDelaySec: 300,
			Routes:       []Route{{Domain: "a.example.com", UpstreamName: "svc-a", Port: 3000, TLS: "acme", Panic: true}},
			Upstreams:    []UpstreamSet{{Name: "svc-a", IPs: []string{"10.129.4.7"}}},
			Certificates: []CertBundle{{Domain: "a.example.com", CertPEM: "c", KeyPEM: "k", NotAfter: 1}}},
		DNS:      []DNSEntry{{Name: "api.internal", IPs: []string{"10.129.4.8"}, Draining: []string{"10.129.4.7"}}},
		Replicas: []ReplicaSpec{{ID: "rep_1", MemoryReservation: 256 << 20, MemorySwap: 512 << 20, CgroupParent: "5050-apps.slice"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"epoch":"ep2"`, `"adopt":true`, `"registryPull"`, `"registryPush"`, `"upstreamName":"svc-a"`, `"panic":true`, `"draining"`, `"cgroupParent"`, `"memoryReservation"`, `"acmePrimary":true`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("encoded desired state lacks %s", key)
		}
	}
	var out DesiredState
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Error("desired state changed across a JSON round trip")
	}
}
