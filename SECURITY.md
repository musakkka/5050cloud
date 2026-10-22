# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: open the repository's **Security** tab and select **Report a vulnerability**. Do not open a public issue.

Include the version, the affected component (control plane, agent, CLI, installer, console), steps to reproduce and the impact you expect.

## What to expect

- We acknowledge a report within 3 working days.
- We confirm or rule out the issue and share a plan within 14 days.
- We aim to release a fix within 90 days of the report, and publish a GitHub security advisory with credit to the reporter unless they prefer otherwise.
- Please give us those 90 days before disclosing publicly. If a fix ships sooner, we coordinate the disclosure date with you.

## Supported versions

Security fixes go into the latest release. Upgrade with `5050 upgrade`.

## Release signing

Releases are signed with minisign. The installer and `5050 upgrade` verify the signature of `checksums.txt` with the public key embedded in them, then the SHA-256 of the archive.

Key rotation: a release signed with the current key ships the next key; the release after that is signed with the next key.

If the signing key is ever compromised, we will publish a security advisory, release a new installer that trusts only the new key, and document the upgrade command with the new key, `5050 upgrade --pubkey <new key>`, here.

## Scope

In scope: everything in this repository. Out of scope: vulnerabilities in the apps you deploy, in your OS or cloud provider, and denial of service by volume against a single install.

The security model is described at [5050cloud.com/docs/reference/security-model](https://5050cloud.com/docs/reference/security-model/).
