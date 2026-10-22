# pkg/configfile (MIT)

Reads config-as-code files (docs/SPEC.md 9.20): `railway.json`, `railway.toml`,
`5050.yaml` and `5050.json` into `manifest.RailwayConfig`, with a map from each
field to its source line, and `nixpacks.toml` into `manifest.NixpacksHints`.

- `Find` and `Load` pick the file in a source tree (the builder, WP4, calls them).
- `Parse` rejects unknown fields with a one-sentence suggestion.
- `Merge` applies a file over a service's settings in Railway's priority order:
  the `environments.<name>` block, then the file's base fields, then the
  service settings. Nixpacks hints fill start and build commands only when
  nothing else sets them. The control plane (WP1) calls it from
  `MergeConfigFile` and shows `Source` as the file icon in the console.
