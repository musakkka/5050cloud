#!/bin/sh
# SPDX-License-Identifier: MIT
#
# restic-env: sourced by the backup and restore scripts that use restic.
# It points restic at s3:<endpoint>/<bucket>/<prefix> (BACKUP_S3_PREFIX is
# "<install prefix>/<service id>") and defines run_restic, which passes the
# bucket lookup style without putting any secret on the command line.

if [ -z "${BACKUP_S3_BUCKET:-}" ] || [ -z "${BACKUP_S3_ENDPOINT:-}" ]; then
	echo "Backups are not configured: set a bucket in Settings > Backups first." >&2
	exit 1
fi
RESTIC_REPOSITORY="s3:${BACKUP_S3_ENDPOINT%/}/${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}"
AWS_DEFAULT_REGION=${BACKUP_S3_REGION:-us-east-1}
RESTIC_CACHE_DIR=${RESTIC_CACHE_DIR:-/tmp/5050-restic-cache}
export RESTIC_REPOSITORY AWS_DEFAULT_REGION RESTIC_CACHE_DIR

run_restic() {
	/opt/5050/bin/restic -o "s3.bucket-lookup=${BACKUP_S3_BUCKET_LOOKUP:-auto}" "$@"
}

# ensure_repo creates the repository on the first backup. Creating it only
# needs put rights, so the write key is enough.
ensure_repo() {
	if run_restic cat config >/dev/null 2>&1; then
		return 0
	fi
	run_restic init >&2
}

# snapshot_id reads the snapshot id from `restic backup --json` output.
snapshot_id() {
	sed -n 's/.*"message_type":"summary".*"snapshot_id":"\([0-9a-f]*\)".*/\1/p' "$1" | tail -n 1
}

# snapshot_size reads the bytes added to the repository from the same output.
snapshot_size() {
	sed -n 's/.*"message_type":"summary".*"data_added":\([0-9]*\).*/\1/p' "$1" | tail -n 1
}
