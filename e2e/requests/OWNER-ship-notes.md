# Notes for the integrator and publisher (from the owner's session)

Builders reported these cross-package gaps. Close them during integration:

- internal/upgrade must export UpgradeAgent, CheckPendingAgentUpgrade and StartControlPlane with the frozen stub signatures; agent/core and orch/engine call them.
- internal/cp/core is missing presence, alerts and settings helpers that other packages use.
- cmd/5050d needs a main that wires orch.New (Box or MasterKeyPath), runs Start, mounts AgentHandler at /api/agent/v1/, and implements the installer argv in WP6-5050d-installer-and-upgrade-hooks.md.
- WP8c screens need routes in web/src/main.tsx: project `settings/:section?`, top-level `/settings/:section?` and `/account/:section?`. Canvas.tsx has type errors that block `npm run build`.
- Pin the SSE frame format for logs and build logs (event:/data: with LogLine JSON) and make the CLI, web and server agree.
- Add @xterm/xterm to web/package.json if it is quick (WP8b request).
- Release signing: no key exists. The publisher generates a minisign key with e2e/scripts/release-key.sh into reference/keys/ (gitignored, chmod 600, never printed or committed), commits only the public key into the installer, signs the v0.1.0 artifacts, and stores the private key as the repo secret MINISIGN_KEY after the repo exists. The installer must verify the published release.
- Delete e2e/requests/ (including this file) before the public squash commit.
