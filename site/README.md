# site

The 5050cloud landing page and docs (`docs/SPEC.md` section 14). MIT licensed. Built to the rules in `reference/design/site-rules.md` (from taste-skill and anti-slop).

```sh
npm run build                       # static site into dist/
npm test                            # builds into a temp dir and checks the acceptance rules
npm run preview                     # serve dist/ on http://127.0.0.1:4321
npm run verify-claims -- e2e/out/<ts>/report.json
```

It needs only Node 22: no npm dependencies. The build reads the 5050cloud source tree for the generated reference pages. It finds it as the parent folder (the normal `site/` checkout), as `../5050cloud`, or wherever `FIFTY_REPO` points. `FIFTY_SITE_REPO` overrides the public repository URL (default in `src/lib/config.mjs`).

`build.mjs` renders the Markdown pages in `src/content/docs/` and generates these pages from the source tree so they cannot drift:

- `docs/reference/api` from `pkg/api/routes.go` (with the descriptions in `docs/SPEC.md` section 11),
- `docs/reference/service-settings` from `pkg/api/types.go` and `data/railway-settings.json`,
- `docs/cli/<command>` from `data/cli.json`,
- `docs/templates/<id>` and `docs/databases/<id>` from `templates/*/template.yaml`.

It also writes a `.md` copy of every page, `/search.json` (the docs search index), `/llms.txt`, `/llms-full.txt`, `/deploy`, `/install.sh` (copied from `install/install.sh`), a sitemap and a 404 page. Links are relative, so the site works at `5050cloud.com` and under a GitHub Pages project path.

## One place for commands and URLs

The install command and the repository URL live in `src/lib/config.mjs`. Markdown pages use them as placeholders, for example `{{INSTALL}}`, `{{GET}}`, `{{INSTALL_WGET}}`, `{{REPO}}`; an unknown placeholder fails the build. Pages name no release dates: work that is not done yet is listed once, under "Not yet" in `docs/reference/honest-limits`, and other pages link there (`[[0.1.1]]` fails the build). The install command is `curl -fsSL https://5050cloud.com/install.sh | sudo sh` (or the `wget -qO-` form); every install command on the site uses 5050cloud.com/install.sh and `npm test` checks that. The site serves that file.

## Claims and prices

Landing claims live in `claims.json`, each with the e2e test that proves it and its condition. A claim backed by tests is stated only when the build is given an e2e report in which all its tests pass (`REPORT_JSON=e2e/out/<ts>/report.json npm run build`, or `data/report.json`). Without one, the page shows the claim's `fallback` (a plain description of the feature with no figure, time or guarantee; `npm test` checks that) or leaves it out: no times, no zero-downtime line, no "Reliable by default" section and no cores figure. The cores figure (E20) comes from the report's measured numbers, never from the target. Prices are in `data/railway-prices.json`, `data/cost-comparison.json` and `data/vm-prices.json`, each dated; the cost calculator on the landing page reads them.

## Demo video and screenshots

- The full demo: `node scripts/media.mjs --demo ../5050cloud-demo/out --reviewed` copies a finished render into `public/demo` (MP4, WebM, captions, chapters, poster). The "Watch the demo" section is built only when those files exist and the MP4 is complete (`findDemo` in `build.mjs`); otherwise the page has no demo section at all, never a placeholder. Run it only after watching the whole render: it must show no private host name, IP, sslip.io name, token or pre-release version.
- The hero is the real console loop (`public/media/hero.*`), trimmed by `scripts/media.mjs` to start at the first console step, with a readable poster (the `${{Postgres.DATABASE_URL}}` reference being typed), a phone picture cropped from the capture itself (`hero-phone`), and WebVTT captions and steps (`hero.en.vtt`, `hero.chapters.vtt`) that the page also prints as a transcript.
- The console stills (canvas, Add menu, metrics, scale, variables, networking, deployments, log explorer) are captures of the current console theme in light and dark at 1440 x 900 (2x), taken from the console running against its MSW mocks (`cd web && VITE_MOCKS=1 npx vite`, the canvas mocks' `orbit` project). `node scripts/media.mjs --stills` remakes only those pictures from `5050cloud-brand/screens` and keeps the hero loop, the terminal recordings and the brand files. The captures keep the console's own colours: its pink accent is the site's accent.
- Every console still has a clean crop that ends at a whole field or the window edge, and most have a 4:3 phone crop (`<name>-<theme>-m-*`) that `picture()` serves below 720 px. The crops are listed in `STILLS` in `scripts/media.mjs`.
- `public/demo/poster.jpg` and `public/screens/*.jpg` are real console screenshots made by `scripts/capture-screens.mjs` against `5050d serve --dev`: it creates a project through the API and leaves every service staged, so no picture shows a deployment result the development server did not really produce. Re-run it after console changes. The strip under the hero shows every file in `public/screens`.
- Screenshot alt text and captions: put them and captions in `data/screens.json` as `{ "file.png": { "alt": "...", "caption": "..." } }`.

## Checks

- `npm test`: pages, links, claims, placeholders, setup guides, the demo slot, public-text rules (no em dashes, banned words or denylisted strings).
- `lint.mjs` and `inspect.mjs` from taste-skill, as in `reference/design/site-rules.md` section 13. `?theme=light` or `?theme=dark` on any page forces a theme for the inspector.
