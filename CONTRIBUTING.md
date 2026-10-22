# Contributing to 5050cloud

Thanks for helping. Bug reports, docs fixes, templates and code are all welcome.

## Before you start

- For a bug, open an issue with the version (`5050 version`, `5050d --version`), the OS, what you did, what you expected and what happened. Include the deployment's diagnosis sentence and log lines, with secrets removed.
- For a feature or a larger change, open an issue first so we can agree on the approach.
- Security issues go through [SECURITY.md](SECURITY.md), not public issues.

## Build and test

You need Go (the version in `go.mod`), Node.js 22 and Docker.

```sh
make deps
make build        # bin/5050d, bin/5050-agent, bin/5050
make test         # Go tests with -race, plus the console's unit tests
make lint
make check-public # secret scan, banned words, licence headers
```

`make dev` runs the control plane, an agent and the console dev server locally. `make help` lists every target. The docs site builds with `cd site && npm run build && npm test` and needs nothing but Node.

## Pull requests

1. Keep each pull request to one change, with tests.
2. Run `make lint test check-public` before you push.
3. Every new source file starts with an `SPDX-License-Identifier` line: `MIT` for `cmd/5050`, `internal/cli`, `pkg/`, `templates/`, `examples/`, `install/` and `site/`; `AGPL-3.0-only` everywhere else. See [LICENSING.md](LICENSING.md).
4. The CLI must not import server code (`internal/cp`, `internal/agent`, `internal/upgrade`, `web`).
5. Update the docs in `site/src/content/docs/` when behaviour changes.

## Writing style

User-facing text (UI, docs, errors) is plain English: short sentences, exact UI names in bold, no em dashes. Error messages are one sentence that says what happened and what to do.

## Templates

New templates live in `templates/<id>/template.yaml` with an icon and a golden render test. See [Writing a template](https://5050cloud.com/docs/contribute/writing-a-template/).

## Licence of contributions

By submitting a contribution you agree that it is licensed under the licence of the directory it changes.

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).
