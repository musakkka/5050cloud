// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"

	"github.com/musakkka/5050cloud/pkg/api"
)

// A worker created by `5050 up` without a port must not wait for a TCP
// answer; anything with a port, a domain or a health path is probed.
func TestProbePort(t *testing.T) {
	var cfg api.ServiceConfig
	def := map[string]string{"PORT": "8080"}
	if got := probePort(cfg, def, false, 8080); got != 0 {
		t.Fatalf("worker with the default PORT probes %d", got)
	}
	if got := probePort(cfg, map[string]string{"PORT": "80"}, false, 80); got != 80 {
		t.Fatalf("detected or user PORT probes %d", got)
	}
	if got := probePort(cfg, def, true, 8080); got != 8080 {
		t.Fatalf("service with a domain probes %d", got)
	}
	withPort := cfg
	withPort.Net.Port = 3000
	if got := probePort(withPort, def, false, 3000); got != 3000 {
		t.Fatalf("service with a target port probes %d", got)
	}
	withPath := cfg
	withPath.Deploy.HealthcheckPath = "/health"
	if got := probePort(withPath, def, false, 8080); got != 8080 {
		t.Fatalf("service with a health path probes %d", got)
	}
}
