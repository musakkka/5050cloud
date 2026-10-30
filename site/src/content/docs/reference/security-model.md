---
title: Security model
description: How 5050cloud protects the console, the hosts, your secrets and your builds.
---

How 5050cloud protects the console, the hosts, your secrets and your builds.

| Area | Rule |
|---|---|
| Exposure | `5050d` listens on 127.0.0.1 only; the world reaches it through Caddy on 443. Agents expose nothing. The registry binds to loopback, the mesh address and the build network gateway, with password auth. Caddy's admin API is a root-only unix socket. |
| Agent identity | Join tokens are single use, expire in 1 hour, are stored hashed and rate limited. Host tokens are 32 random bytes, stored hashed, bound to the host's machine-id and WireGuard key, and rotated every 30 days. A clone that gets a new machine-id or key is refused; an exact copy of a host, with its token, is not told apart yet. Removing a host revokes it. |
| Secrets at rest | Variables, registry, bucket and SMTP credentials, the GitHub App key, webhook and TOTP secrets are encrypted with XChaCha20-Poly1305 under `/etc/5050/master.key` (0600). The agent's cache is encrypted with the host key. `5050d rotate-master-key` rotates. |
| Secrets in transit | Never sent to the browser except through an audited reveal. Agents receive resolved variables only for replicas they run. Clone tokens are short-lived and never in URLs or logs. Build variables never appear on a command line. |
| Backups | Encrypted (WAL-G libsodium, restic, age). Data containers hold a write-only bucket key; deletes use a separate retention key on the control host. |
| Containers | No `--privileged`, no Docker socket mounts, default seccomp and AppArmor, `no-new-privileges`, default capabilities. No access to host addresses, the mesh or the metadata endpoint by default. |
| Builds | BuildKit in a container on its own bridge; RUN steps cannot reach host addresses or metadata. |
| Commands | Every Docker and host command is an argument list, never a shell string. |
| Web | HttpOnly, Secure, SameSite cookies; a CSRF header on every state change; a strict Content Security Policy; no CORS; rate-limited login on the real client IP; argon2id passwords; TOTP 2FA; constant-time token checks. |
| SQL | Parameters only. There is no free query box. |
| Outbound requests | Webhooks, the bucket test and GitHub fetches check the resolved address at connect time and refuse private, loopback and metadata ranges. |
| Supply chain | All versions pinned; tool downloads verified by SHA-256; releases signed with minisign and verified by the installer and the upgrader. |
| Audit | Every change, reveal, login, failed login, 2FA change, shell, tunnel and host join. |

## Not in v0.1

Mutual TLS between agents and the control plane. The channel is TLS to the console certificate plus a bound, rotated host token.

## Report a vulnerability

See [SECURITY.md]({{REPO}}/blob/main/SECURITY.md).
