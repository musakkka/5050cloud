# pkg/manifest

File formats, frozen for v0.1 (docs/SPEC.md 10, 12, 9.20). MIT licensed.

- `Template`: `templates/<id>/template.yaml`.
- `ProjectFile`: `5050.yaml` as written by `5050 config pull`.
- `RailwayConfig`: `railway.json`, `railway.toml`, and the config-as-code part
  of `5050.yaml`/`5050.json`; pointer fields tell "set by the file" from
  "not set".
- `NixpacksHints`: what `pkg/configfile` keeps from `nixpacks.toml`.

Every YAML file goes through `sigs.k8s.io/yaml` (YAML to JSON, then the
`json` tags), so field names are camelCase in both formats. The types have no
`yaml` tags; never decode them with `gopkg.in/yaml.v3`, which lower-cases
untagged names. Note that `encoding/json` matches keys case-insensitively, so
`pkg/configfile` checks exact spelling itself before it suggests a fix.

`golden_test.go` decodes the fixtures in `testdata/` strictly, encodes them
and compares with the `.golden.yaml` files, checks that a second round trip
is stable, and fails when any JSON field name of these types is set by no
fixture. Run `go test ./pkg/manifest -update` after a reviewed contract
change and read the diff.
