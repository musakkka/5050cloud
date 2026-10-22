#!/bin/sh
# SPDX-License-Identifier: MIT
#
# restore-volume: restores snapshot BACKUP_ID into the EMPTY volume mounted at
# /data (a new volume for the restored copy).
#
#   restore-volume                 restore
#   restore-volume --check         also check that the snapshot restored files
set -eu
. /opt/5050/bin/restic-env

if [ -z "${BACKUP_ID:-}" ]; then
	echo "No snapshot was given to restore (BACKUP_ID is empty)." >&2
	exit 1
fi
if [ "${FIFTY_RESTORE_WIPE:-}" = "1" ]; then
	find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi
if [ -n "$(ls -A /data 2>/dev/null | grep -v '^lost+found$' || true)" ]; then
	echo "The target volume is not empty, so the restore stopped to protect it." >&2
	exit 1
fi
run_restic restore "$BACKUP_ID" --target / >&2
if [ "${1:-}" = "--check" ]; then
	run_restic check --read-data-subset=5% >&2
fi
echo '{"ok":true}'
