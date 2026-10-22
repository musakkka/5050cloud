#!/bin/sh
# SPDX-License-Identifier: MIT
#
# wipe-volume: empties the volume mounted at /data (Wipe Volume, docs/SPEC.md
# 9.12). The service is stopped before this runs and started again after.
set -eu
if [ ! -d /data ]; then
	echo "The volume is not mounted at /data." >&2
	exit 1
fi
find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +
echo '{"ok":true}'
