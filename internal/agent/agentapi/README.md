# internal/agent/agentapi

Interfaces between the agent's parts (docs/SPEC.md section 8), frozen for
v0.1: `Tools` (core), `Builder` (WP4, `internal/agent/build`), `Network` and
`Ingress` (WP5, `internal/agent/netw`). The runtime (WP3,
`internal/agent/core`) calls them in the order given in the package comment,
both for a fresh desired state and when it boots from its local cache.

`Ingress.SetUpstreams` changes the answers of the upstream resolver that
Caddy's dynamic upstreams read, so deploys and failovers never reload Caddy;
`Ingress.Apply` reloads only when the route set or certificates change.
