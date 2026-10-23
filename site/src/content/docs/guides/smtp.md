---
title: Email (SMTP)
description: Connect an SMTP server so invites, password resets, email verification and alerts go out by email.
---

Connect an SMTP server so invites, password resets, email verification and alerts go out by email.

## Pick a provider

Any SMTP relay works: your email provider's SMTP access or a transactional email service. You need a host name, a port, a user name and password, and a from address the provider lets you send as.

- Port 587 with STARTTLS is the usual choice.
- Port 465 means TLS from the first byte: pick **TLS**.
- Many clouds block outbound port 25. Do not use it.

## Set it

1. In the console, go to **Settings**, **General**, and find the **Email** section.
2. Fill in **SMTP host** (for example `smtp.example.com`), **Port** and **TLS** (**STARTTLS**, **TLS** or **None**).
3. Fill in **User name** and **Password**. Later, leave **Password** empty to keep the stored one.
4. Set **From address**, for example `5050cloud <noreply@example.com>`.
5. Select **Save**.

## Test it

1. In the same section, type your address in **Send a test email to**.
2. Select **Send test email**.
3. The console shows "Test email sent." or the error the SMTP server returned. Check your inbox and spam folder.

## Help your mail arrive

At the DNS provider for your from address's domain, add the SPF and DKIM records your SMTP provider gives you. Without them, many inboxes file the mail as spam or reject it.

## What changes once email is set

- Invites in **Settings**, **Members** can go out by email instead of as a link to copy.
- **Forgot password** on the login page sends a reset link.
- New users get a link to verify their address. Notification emails go to verified addresses only.
- **Account**, **Notifications** offers an email choice for each event, and install alerts in **Settings**, **General**, **Alerts** can go to a list of addresses.

## Without SMTP

Everything still works. Invites give you a link to copy and send yourself. For a forgotten password, run this on the control host and send the person the one-time link it prints:

```sh
sudo 5050d reset-password --email you@example.com
```

## Troubleshooting

- **Connection refused or timed out.** The port is blocked outbound by your cloud provider, or the host name is wrong. Try port 587, and check from the control host with `nc -zv smtp.example.com 587`.
- **Certificate or handshake errors.** The **TLS** mode does not match the port. Use **STARTTLS** on 587 and **TLS** on 465.
- **Sender rejected.** The from address is not one your provider lets you send as. Verify it with the provider, or use the address they give you.
- **The test passes but mail never arrives.** Add SPF and DKIM records, then check the spam folder.

## What success looks like

- The test email arrives.
- An invite sent by email arrives.
- **Account**, **Notifications** shows email toggles.

## Next steps

- [Email and notifications](/docs/guides/email-notifications): alerts and signed webhooks.
- [Members and invites](/docs/guides/members-roles-2fa)
