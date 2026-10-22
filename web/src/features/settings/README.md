# WP8c: settings, hosts, members, account, usage

Screens for the WP8c rows of docs/SPEC.md 13.1.

| Route | Component |
|---|---|
| `/settings/:section` | `settings/InstallSettings.tsx`: General (install, Email/SMTP with Send test email, Alerts), Hosts, GitHub, Registry credentials, Backups, Members, API tokens, Upgrades, Audit log |
| `/project/:pid/settings/:section` | `project-settings/ProjectSettingsPage.tsx`: General, Usage, Environments, Shared Variables, Webhooks, Members, Tokens, Danger |
| `/account/:section` | `account/AccountPage.tsx`: Profile, Security (password, 2FA, recovery codes), Notifications |

`settings/routes.tsx` exports the three route elements. The app router in `src/main.tsx` (WP8a)
mounts them as shown in that file.

Code layout:

- `settings/client.ts`: typed calls to `/api/v1` (types from `src/api/types.gen.ts`), with
  `X-5050-CSRF: 1` on every mutation and `api.Error` turned into `ApiError`.
- `settings/forms.ts`: pure form checks and request builders, one sentence per error.
- `settings/ui.tsx`: page shell, fields, `useLoad`, `useAction` and `optimistic` (apply at once,
  roll back with the error on failure).
- `hosts/`, `members/`, `backups-settings/`, `usage/`, `account/`: one screen each.

Tests: `npx vitest run src/features/settings` (form checks, client against the generated MSW
fixtures, small helpers).

## Later work

- Playwright specs under `web/e2e/specs/{settings,hosts,members,usage}` for U12, U13, U14, U30 to
  U33, U40 and U41.
- A QR code for the 2FA setup; today the page shows the setup key and an `otpauth://` link.
- A per-session list with revoke under Account > Security (the API has no sessions endpoint yet;
  the page offers Sign out).
- Per-host metrics charts on Settings > Hosts (`/hosts/{id}/metrics`).
- Adopt mode screens for unknown replicas after a restore (`/restore/unknown-replicas`).
- Raw editor and sharing to services on Shared Variables.
- Registry cap, build timeout and Railway alias settings on Settings > General.
