# internal/agent/netw

Agent networking and ingress (docs/SPEC.md section 8.4, work package WP5).
It implements two frozen interfaces from `internal/agent/agentapi`:

- `netw.New(Options)` returns an `agentapi.Network`: Docker networks per
  environment, the WireGuard mesh, the private DNS servers and the firewall.
- `netw.NewIngress(IngressOptions)` returns an `agentapi.Ingress`: Caddy on
  the host network, the upstream resolver Caddy reads replica addresses from,
  TCP proxies, certificate status and HTTP access logs.

`netw.Restore(ctx, Options, proto.DesiredState)` is what
`5050-net-restore.service` runs before Docker starts: it applies the firewall
and the mesh from the cached desired state and touches neither Docker nor DNS,
so a quarantined environment never has a window of open egress after a reboot.

Nothing touches the host until the first `Apply`.

## Files

| File | What it does |
|---|---|
| `addrplan.go` | Address plan: host /16s, environment /24 slots, build network, mesh addresses, slot limits and the "No network slot left" message. |
| `dockernet.go` | One bridge per environment per host (`5050-<env id>`, bridge `f5-<hash>`), ICC on, MTU set to the mesh MTU when the mesh is on. Stale 5050 networks are removed; unlabelled ones are never touched. |
| `firewall.go`, `iptables.go` | Renders our chains (`5050-ISOLATE`, `5050-EGRESS`, `5050-NAT`, `5050-HOSTIN` plus one small chain per environment) and applies them with one `iptables-restore --noflush`. Rules outside our chains are left alone; unchanged rules are not rewritten. MSS is clamped to path MTU on `wg5050`. |
| `mesh.go`, `mesh_linux.go` | WireGuard on `wg5050`: private key in `<DataDir>/wg.key` (0600, never sent), peers and allowed IPs from `proto.Mesh`, routes per peer /16, keepalive 25 s, last handshakes and peers that cannot reach each other. |
| `dns.go`, `forward.go` | A DNS server per environment gateway (UDP and TCP 53) answering `<name>.internal`, `<name>.railway.internal` and rename aliases with healthy replicas (shuffled, TTL 5), never draining ones. Other names go to the host resolvers from `/etc/resolv.conf`, or `/run/systemd/resolve/resolv.conf` when only the systemd stub is listed. The upstream resolver on `127.0.0.1:5354` answers `<name>.upstream.5050`. |
| `caddyconfig.go`, `caddyadmin.go`, `ingress.go` | Caddy lifecycle (pinned image, `--resume`, admin on a unix socket) and one rendered JSON config: HTTP redirects to HTTPS, one route per domain with dynamic `a` upstreams through our resolver (refresh 1 s), random selection, 5 s try duration, passive health checks unless the route is in panic mode, `stream_close_delay 5m`, `X-Real-Ip`, JSON access logs, a styled 404 catch-all, Let's Encrypt then ZeroSSL (or a custom ACME directory) and the internal issuer. Non-primary ingress hosts proxy `/.well-known/acme-challenge/*` to the primary over the mesh and load pushed certificates as static ones. `POST /load` happens only when the rendered config changes. |
| `certs.go` | Certificate status per domain with readable error sentences, and the certificates the ACME primary issued (reported as `cert_issued`). |
| `tcpproxy.go` | A Go listener per allocated host port, round robin over upstreams, idle timeout 1 hour; new listeners open before removed ones close. |
| `accesslog.go`, `httpstats.go` | Tails Caddy's access log (position kept across restarts), turns lines into `proto.LogRecord` and aggregates `proto.HTTPSample` per service. |
| `pins.go` | The pinned Caddy image digest. |

Design note: environment membership is matched with one small iptables chain
per environment instead of an ipset, so hosts need no `ipset` tool and the
whole rule set stays inside one atomic `iptables-restore`.

## Tests

Unit tests, with golden files in `testdata/` for iptables and Caddy JSON
(run anywhere):

    go test -cover ./internal/agent/netw/...

Regenerate the golden files after an intended change:

    go test ./internal/agent/netw/ -run Golden -update

Linux integration tests (two network namespaces joined by WireGuard as two
hosts, DNS reach within an environment, blocks across environments, to host
addresses and to metadata, egress allowlist, MSS clamp, restore before
Docker, Caddy upstream swaps under load with zero errors and no reload, and a
websocket surviving a route set reload). They need root, so run them in the
privileged container:

    make test-linux TEST_LINUX_PKGS="./internal/agent/netw/... -run Integration -v"

The Caddy test pulls the pinned image from Docker Hub. Set
`NETW_NO_INSTALL=1` to skip instead of installing iptables and iproute2 into
the test container.
