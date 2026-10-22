#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# web-size.sh: the P10 budget of docs/SPEC.md 16.1. The JavaScript that
# web/dist/index.html loads up front (module scripts and modulepreload links)
# may be at most 300 KB gzipped. Lazy chunks do not count.
set -eu

cd "$(dirname "$0")/../web/dist"
budget=$((300 * 1024))

if [ ! -f index.html ]; then
	echo "web/dist/index.html is missing. Run make web first." >&2
	exit 1
fi

files=$(grep -oE '(src|href)="/assets/[^"]+\.js"' index.html | sed -E 's/^(src|href)="\/(.*)"$/\2/' | sort -u)
if [ -z "$files" ]; then
	echo "web/dist/index.html references no JavaScript under /assets; the build output changed shape." >&2
	exit 1
fi

total=0
for f in $files; do
	n=$(gzip -9 -c "$f" | wc -c | tr -d ' ')
	total=$((total + n))
done

kb=$((total / 1024))
if [ "$total" -gt "$budget" ]; then
	echo "Initial JavaScript is ${kb} KB gzipped, over the 300 KB budget (P10). Lazy-load xterm, charts and the icon set." >&2
	exit 1
fi
echo "Initial JavaScript is ${kb} KB gzipped (budget 300 KB)."
