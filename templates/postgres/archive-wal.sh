#!/bin/sh
# SPDX-License-Identifier: MIT
#
# archive-wal: Postgres archive_command (docs/SPEC.md 9.13).
# While no backup bucket is configured, or archiving is switched off for this
# database (a restored copy before Promote), it exits 0 without uploading.
# Otherwise it uploads the segment with WAL-G and exits non-zero on failure,
# so Postgres keeps the segment and tries again.
set -eu

if [ -z "${WALG_S3_PREFIX:-}" ] || [ "${FIFTY_ARCHIVE:-on}" = "off" ]; then
	exit 0
fi
if [ "$#" -ne 1 ]; then
	echo "archive-wal needs exactly one argument, the path of the WAL segment." >&2
	exit 2
fi
exec /opt/5050/bin/wal-g wal-push "$1"
