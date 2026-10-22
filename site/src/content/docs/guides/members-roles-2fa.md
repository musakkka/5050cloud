---
title: Members, roles and 2FA
description: Invite people with a role, limit them per project, and require two-factor authentication.
---

Invite people with a role, limit them per project, and require two-factor authentication.

## Invite

**Settings > Members > Invite** by email (with SMTP), or create an invite link with a role, an optional project, a use limit and an expiry (default 7 days).

## Roles

| Action | Owner | Admin | Member | Viewer |
|---|:-:|:-:|:-:|:-:|
| View projects, canvas, deployments, logs, metrics | yes | yes | yes | yes |
| Reveal variables, `5050 run`, shell, connect | yes | yes | yes | no |
| Create and change services, variables, domains, volumes; deploy, roll back | yes | yes | yes | no |
| Delete services, environments, projects; restore and promote databases; wipe volumes | yes | yes | no | no |
| Hosts, GitHub App, bucket and SMTP settings, members, upgrades, audit log | yes | yes | no | no |
| Transfer ownership, reveal recovery key | yes | no | no | no |

**Project Settings > Members** can grant a person `editor`, `viewer` or `none` for one project, replacing their install role there. Owners and Admins always keep full access.

## Two-factor authentication

1. Open the avatar menu, then **Security**.
2. Scan the QR code with an authenticator app and enter a code.
3. Save the 10 recovery codes.

Admins can require 2FA for everyone in **Settings > Members**. If someone loses their device, run `sudo 5050d reset-2fa --email them@example.com` on the control host.

## Passwords

At least 10 characters. Forgot password sends a reset link by email; without SMTP, run `sudo 5050d reset-password --email you@example.com` on the control host for a one-time link.

## Audit log

**Settings > Audit** lists every change, reveal, login, shell session and host join, with who, what and from which IP. Values are never logged.

## Public projects

**Project Settings > General > Visibility** can make a project read-only public: anyone with the link sees the canvas, statuses and deployments, never variables, logs or settings.
