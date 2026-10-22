# pkg/proto

The wire protocol between `5050d` and `5050-agent` (docs/SPEC.md section 7),
frozen for v0.1. MIT licensed so fake agents and tools can speak it.

One WebSocket per agent, dialled out by the agent. Text frames carry one JSON
`Envelope` (`t` type, `id`, `ref`, `d` payload); binary frames carry exec and
TCP stream bytes. The control plane pushes a full `DesiredState` per host
(generation, epoch, adopt flag) and the agent reconciles to it idempotently;
everything else is a request with an id and a reply that refers to it. The
package comment in `proto.go` describes keepalive, reconnect, the generation
and epoch guard and adopt mode.

`proto_test.go` checks that every field has a camelCase JSON tag, that
message type names are unique, and that the fields M0 added survive a JSON
round trip.
