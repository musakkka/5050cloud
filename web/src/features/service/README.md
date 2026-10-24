# WP8b: service panel, deployment view, logs, metrics, data, volume

docs/SPEC.md 13.1 (WP8b rows), 13.3, 13.5. The shell (WP8a) mounts the
exports of `routes.tsx`: `ServiceRoute`, `VolumeRoute`, `LogExplorer`,
`ObservabilityRoute`, each given the open environment id.

| Directory | What |
|---|---|
| `service/` | Panel header and tabs, route components, fetch client (`client.ts`, CSRF header), events hook, shared UI primitives |
| `deployment/` | Deployments tab (meta row, active/running/failed card with step list, no-deployment card with Deploy, history with Hide Skipped, kebab actions with confirms) and the deployment drawer stacked over the panel (Details with steps, diagnosis, variables, origin and Pretty/Code config; Build and Deploy Logs with filter tags, view options, line pane, live tail; Network Logs with HTTP, DNS and Flow). `mocks.ts` holds richer MSW fixtures for `npm run dev:mocks` |
| `logs/` | Live log view (SSE tail, pause on scroll with Resume, columns, side panel with filter-on-click, View in Context, dropped-lines warning, host notices) and the Log Explorer |
| `variables/` | Variables tab, Raw Editor parser (`.env` and JSON) with preview, `${{` autocomplete, seal, reveal, Heroku import, shared variables, database reference buttons |
| `metrics/` | Metrics tab (synced cursors, deploy markers, Shift+drag zoom with Back, ranges, custom range, Sum/Replica, monitors, Postgres extras) and Observability |
| `service-settings/` | Settings: Source, Networking (domains, DNS records, TCP proxy), Build, Deploy, Scale, Danger; staged colour; config-file fields read-only |
| `cron/` | Cron parser with next runs in a time zone, presets, Cron Runs tab with Run now |
| `backups-tab/` | Backups: health, Back up now, schedule, lock, delete, restore to a copy (backup or point in time), promote |
| `data/` | Data tab (tables, collections, Postgres extensions, Redis keys), Connect modal, Regenerate credentials |
| `volume/` | Volume panel (usage, rename, mount path, alert, Wipe Volume, delete) and `AttachVolumeModal` for the canvas |
| `shell/` | Shell tab over the shell WebSocket with a small terminal model |

Tests: `npm test` runs the unit tests for the Raw Editor parser, cron,
log helpers, metrics helpers, deployment actions, settings patches and the
terminal model.

## Later (not in this build)

- Shell: swap the built-in terminal for xterm.js once the dependency lands
  (`e2e/requests/WP8b-xterm-dependency.md`).
- Playwright specs under `web/e2e/specs/{service,deployment,logs,metrics,data}`
  for U4, U6 to U10, U15 to U18, U20 to U25, U29, U42 (need a real install).
- Activity tab for a single service (the project Activity panel covers it).
- Settings: inline Connect GitHub from the Source picker (links to the
  GitHub App setup instead), host picker for pinned placement (host ids
  typed one per line).
- Use WP8a's shared kit (`src/kit`) and `StatusGlyph` in place of
  `service/ui.tsx` once both packages are merged.
