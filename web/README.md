# web

The 5050cloud console: React 18, Vite 6, Tailwind 3 (docs/SPEC.md 13).
AGPL-3.0-only. Package name `@5050cloud/web`.

M0 copied this tree from the earlier internal console and removed every
private string listed in the reuse audit. Most screens still read the old
response shapes in `src/api.ts`; WP8a moves them to the generated contract
in `src/api/types.gen.ts` and owns this tree from here.

| Command | What |
|---|---|
| `npm run dev` | Vite on :5173, proxying `/api` to `FIFTY_API` (default `http://127.0.0.1:5050`) |
| `npm run dev:mocks` | Vite with every API call answered by the generated MSW mocks (`src/api/mocks`) |
| `npm run build` | Type check and build into `dist/` (embedded by `web/embed.go`) |
| `npm run lint`, `npm run typecheck`, `npm test` | ESLint, `tsc --noEmit`, Vitest |

Generated files (do not edit): `src/api/types.gen.ts` (tygo over `pkg/api`),
`src/api/mocks/handlers.gen.ts` (`pkg/api/internal/mockgen`), both from
`make gen-ts`, and `e2e/labels.json` (`make labels`).

Local preferences live in `localStorage` under the `5050.` prefix
(`5050.stars`, `5050.activity`; `5050.avatars` = `"0"` turns off GitHub
avatars, which are a third-party request).
