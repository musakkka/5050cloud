# WP8a: vitest include, e2e script and main.tsx entry for the console

- From: WP8a
- To: integrator
- Files: web/vite.config.ts, web/package.json, web/src/main.tsx, web/src/pages, web/src/ui, web/src/api.ts
- Blocking: no (WP8a works around each item inside its own directories)

## Why
docs/SPEC.md 13 and 13.8. The new console lives in web/src/app, web/src/kit
and web/src/features; the Playwright suite lives in web/e2e. A few shared
files outside WP8a's directories need small changes.

## Proposed change
1. `web/src/main.tsx` (already edited, one import): it now only calls
   `boot()` from `src/app/main.tsx`. Please keep that change.
2. `web/src/pages`, `web/src/ui` and `web/src/api.ts` are the old console
   copied by M0. Nothing imports them any more except the WP8b/WP8c files
   that still use `../../ui/kit`; once those move to `src/kit`, delete the
   three paths.
3. `web/vite.config.ts`: add `test: { include: ["src/**/*.test.{ts,tsx}"] }`
   so Vitest never collects Playwright files. Until then the Playwright specs
   are named `*.e2e.ts` (web/e2e/playwright.config.ts matches that), which
   Vitest ignores.
4. `web/package.json`: add `"test:e2e": "npm --prefix e2e test"`. The suite
   has its own `web/e2e/package.json` pinning `@playwright/test` 1.56.1, so
   the console's dependencies and bundle stay unchanged.
5. `web/index.html`: drop `class="dark"` from `<html>`; `src/kit/theme.ts`
   sets `light` or `dark` before the first render (system default).
