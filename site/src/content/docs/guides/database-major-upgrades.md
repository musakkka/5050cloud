---
title: Database major upgrades
description: Move a database to a new major version by restoring a dump into a copy, then promoting it.
---

Move a database to a new major version by restoring a dump into a copy, then promoting it.

## Why not just change the image

A new major version cannot read the old data directory. Changing the major version on an existing volume is refused with "Major version changes need a dump and restore. Use Upgrade to a new major version in the database panel."

## Steps

1. Open the database panel menu and select **Upgrade to a new major version**.
2. Pick the version (for Postgres, 17 or 18).
3. 5050cloud dumps the source (`pg_dumpall` for Postgres, `mongodump` for MongoDB) and restores into a new copy on the chosen version. The source keeps running.
4. Check the copy, then select **Promote**. It takes the original's name, dependents redeploy, and the original stops as `<name>-old-<stamp>`.

Stop writes during the dump if you cannot lose any.

## Patch releases

The panel checks weekly for a newer patch of the same major and shows, for example, "Postgres 17.6 is available" with **Update**. That is a normal redeploy with a short gap.
