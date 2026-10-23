---
title: Members, roles and 2FA
nav: Members and invites
description: Invite people with a role, limit them per project, turn on two-factor authentication, and read the audit log.
---

Invite people with a role, limit them per project, turn on two-factor authentication, and read the audit log.

## Invite someone

1. In the console, go to **Settings**, **Members** and select **Invite**. You need the **Admin** or **Owner** role.
2. In **Invite people**, type their **Email**, or leave it empty to create a link.
3. Pick a **Role**: Admin, Member or Viewer.
4. Set **Uses** (default 1) and **Expires after (hours)** (default 168, which is 7 days).
5. Select **Create invite**.

With [SMTP](/docs/guides/smtp) set and an email given, the console says "We emailed an invite to" the address. Otherwise it shows the invite link with how many times it works and when it expires: copy it and send it yourself. The person opens the link, sets their name and password, and lands in the console.

**Pending invites** lists invites not used yet; **Revoke** cancels one.

## Roles

| Action | Owner | Admin | Member | Viewer |
|---|:-:|:-:|:-:|:-:|
| View projects, canvas, deployments, logs, metrics | yes | yes | yes | yes |
| Reveal variables, `5050 run`, shell, connect | yes | yes | yes | no |
| Create and change services, variables, domains, volumes; deploy, roll back | yes | yes | yes | no |
| Delete services, environments, projects; restore and promote databases; wipe volumes | yes | yes | no | no |
| Hosts, GitHub App, bucket and SMTP settings, members, upgrades, audit log | yes | yes | no | no |
| Transfer ownership, reveal recovery key | yes | no | no | no |

## Access per project

In a project, **Project Settings**, **Members** gives a person `editor`, `viewer` or `none` for that project, in place of their install role there. Owners and Admins always keep full access.

## Two-factor authentication

1. Open your avatar menu, then **Security** (under **Account**).
2. Select **Set up two-factor authentication**.
3. Add the **Setup key** to your authenticator app, or select **Open in authenticator app** on a phone that has one.
4. Type the **Code from the app** and select **Turn on**.
5. Save the recovery codes the console shows somewhere outside the console.

To turn it off, enter your **Password** and a **Code**, then select **Turn off**.

Lost the device and the recovery codes? An admin can run this on the control host:

```sh
sudo 5050d reset-2fa --email them@example.com
```

## Require 2FA for everyone

**Settings**, **Members** has **Require two-factor authentication for everyone**. People without 2FA are asked to turn it on, and anyone who has it cannot turn it off.

> **Not yet:** the setting is stored and shown, but login does not refuse a person who has not turned 2FA on. Check the member list yourself. [What is not in this release](/docs/reference/honest-limits#not-yet).

## Passwords

At least 10 characters. **Forgot password** on the login page sends a reset link by email. Without SMTP, run `sudo 5050d reset-password --email you@example.com` on the control host for a one-time link.

## Transfer ownership

1. In **Settings**, **Members**, open the person's **More** menu and select **Transfer ownership**.
2. Enter **Your password**, and your **Two-factor code** if you use 2FA.
3. Select **Transfer ownership**. They become the owner and you become an Admin.

## Audit log

**Settings**, **Audit log** lists every change, reveal, login, shell session and host join, with who, what and from which IP. Values of variables are never logged.

## Public projects

**Project Settings**, **General**, **Visibility** can make a project public and read-only: anyone with the link sees the canvas, statuses and deployments, never variables, logs or settings.

## What success looks like

- The invited person appears in **Settings**, **Members** with the role you picked.
- **Settings**, **Audit log** shows the invite and its acceptance.
- After you turn on 2FA, the next login asks for a code.

## Next steps

- [Email (SMTP)](/docs/guides/smtp)
- [Security model](/docs/reference/security-model)
