---
title: Quarantined environments
description: Block outbound internet traffic from an environment, with an allowlist, while its private network keeps working.
---

Block outbound internet traffic from an environment, with an allowlist, while its private network keeps working.

## Turn it on

1. Open **Project Settings > Environments** and pick the environment.
2. Turn on the **Quarantine** toggle, which sets egress to blocked.
3. Add allowed destinations (IP addresses or ranges) if the services need some.

Containers in that environment can still reach each other over the private network. Everything else outbound is rejected.

## Guarantees

- Rules for a new container are in place before it joins any route.
- After a host reboot, the firewall rules are restored from the agent's cache before Docker starts any container, so there is no window of open egress.

## Uses

Run untrusted code, keep a staging copy of production data from calling real third parties, or meet a compliance rule.
