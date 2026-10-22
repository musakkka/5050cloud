# site

The 5050cloud landing page and docs (`docs/SPEC.md` section 14). MIT licensed.

```sh
npm run build                       # static site into dist/
npm test                            # builds into a temp dir and checks the acceptance rules
npm run preview                     # serve dist/ on http://127.0.0.1:4321
npm run verify-claims -- e2e/out/<ts>/report.json
```

It needs only Node 22: no npm dependencies. `build.mjs` renders the Markdown pages in `src/content/docs/`, and generates these pages from the source tree so they cannot drift:

- `docs/reference/api` from `pkg/api/routes.go` (with the descriptions in `docs/SPEC.md` section 11),
- `docs/reference/service-settings` from `pkg/api/types.go` and `data/railway-settings.json`,
- `docs/cli/<command>` from `data/cli.json`,
- `docs/templates/<id>` and `docs/databases/<id>` from `templates/*/template.yaml`.

It also writes a `.md` copy of every page, `/llms.txt`, `/llms-full.txt`, `/deploy` (the Deploy on 5050cloud button target), `/install.sh` (copied from `install/install.sh`), a sitemap and a 404 page. Links are relative, so the site works at `5050cloud.com` and under a GitHub Pages project path.

Landing claims live in `claims.json`, each with the e2e test that proves it and its condition. The E20 number is shown when a report is given at build time (`REPORT_JSON=path npm run build`, or `data/report.json`). Screenshots in `public/screens/` are shown in the Screens section when present.

## Later

- Screenshots from the UI suite (`web/e2e`) go in `public/screens/` (light and dark: canvas, deploy logs, variables with references, metrics). The section is hidden until they exist.
- SPEC 14 names Astro Starlight. This first version is a dependency-free generator because the build had to work offline; moving to Starlight later keeps the same Markdown pages.
- Lighthouse runs are not automated here yet.
