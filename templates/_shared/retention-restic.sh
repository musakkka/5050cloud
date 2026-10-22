#!/bin/sh
# SPDX-License-Identifier: MIT
#
# retention-restic: removes the snapshots named in FORGET_IDS (space
# separated) and prunes unreferenced data. The control plane picks the ids
# from its backup list (older than the retention window and not locked), so
# locked backups are never touched. Runs on the control host with the
# retention key, the only key that may delete.
set -eu
. /opt/5050/bin/restic-env

if [ -z "${FORGET_IDS:-}" ]; then
	echo '{"ok":true}'
	exit 0
fi
# shellcheck disable=SC2086 # FORGET_IDS is a list of hex ids
run_restic forget --prune $FORGET_IDS >&2
echo '{"ok":true}'
