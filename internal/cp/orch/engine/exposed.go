// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/musakkka/5050cloud/pkg/api"
)

// detectImagePort applies the "detected port" rule of docs/SPEC.md 9.3 to
// image services: when the service has no target port and PORT is still the
// default, the image's only exposed TCP port (EXPOSE) becomes PORT for this
// deployment, so the health check and the variable match what the image
// listens on (traefik/whoami and nginx listen on 80). Any failure keeps the
// default; the build itself reports image errors.
func (o *Orchestrator) detectImagePort(ctx context.Context, d depRow, cfg api.ServiceConfig, env, provided map[string]string) {
	if cfg.Source.Image == "" || (cfg.Source.Type != "" && cfg.Source.Type != "image") || cfg.Net.Port > 0 {
		return
	}
	if env["PORT"] != provided["PORT"] {
		return // the user set PORT
	}
	ref, err := name.ParseReference(strings.TrimSpace(cfg.Source.Image))
	if err != nil {
		return
	}
	auth := authn.Anonymous
	if cfg.Source.RegistryCredential != "" {
		u, p, _, err := o.cfg.RegistryCredential(ctx, cfg.Source.RegistryCredential)
		if err != nil {
			return
		}
		auth = authn.FromConfig(authn.AuthConfig{Username: u, Password: p})
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	img, err := remote.Image(ref, remote.WithContext(cctx), remote.WithAuth(auth))
	if err != nil {
		return
	}
	cf, err := img.ConfigFile()
	if err != nil || cf == nil {
		return
	}
	port := onlyTCPPort(cf.Config.ExposedPorts)
	if port == 0 || strconv.Itoa(port) == env["PORT"] {
		return
	}
	env["PORT"] = strconv.Itoa(port)
	_ = o.builds.Append(d.ID, o.now().UnixMilli(), []string{fmt.Sprintf("The image exposes port %d, so PORT is %d. Set a target port in Settings > Networking to change it.", port, port)})
}

// onlyTCPPort returns the single exposed TCP port, or 0 when there are none
// or several.
func onlyTCPPort(exposed map[string]struct{}) int {
	var ports []int
	for k := range exposed {
		p, proto, _ := strings.Cut(k, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)
	if len(ports) != 1 {
		return 0
	}
	return ports[0]
}
