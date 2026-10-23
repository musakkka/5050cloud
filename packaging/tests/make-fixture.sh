#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Builds a fake, signed CLI release the way .goreleaser.yaml lays it out:
#
#   OUT/latest.txt
#   OUT/vX.Y.Z/5050-cli_X.Y.Z_{darwin,linux}_{amd64,arm64}.tar.gz
#   OUT/vX.Y.Z/checksums.txt and checksums.txt.minisig (signed with e2e/keys/test.key)
#
#   tests/make-fixture.sh SRC OUT [VERSION]
#
# SRC is a checkout of the 5050cloud repository; nothing in it is changed.
set -eu
src=${1:?usage: make-fixture.sh SRC OUT [VERSION]}
out=${2:?usage: make-fixture.sh SRC OUT [VERSION]}
ver=${3:-v0.0.1-test}
v=${ver#v}
src=$(cd "$src" && pwd)
mkdir -p "$out/$ver" "$out/.bin" "$out/.stage"
out=$(cd "$out" && pwd)

mod=$(sed -n 's/^module //p' "$src/go.mod")
(cd "$src" && go build -o "$out/.bin/signtool" ./e2e/signtool)
for t in darwin_arm64 darwin_amd64 linux_arm64 linux_amd64; do
	os=${t%_*}; arch=${t#*_}
	d=$out/.stage/$t
	mkdir -p "$d"
	(cd "$src" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X $mod/pkg/version.Version=$ver -X $mod/pkg/version.Commit=test -X $mod/pkg/version.Date=test" \
		-o "$d/5050" ./cmd/5050)
	cp "$src/pkg/LICENSE" "$d/LICENSE"
	COPYFILE_DISABLE=1 tar --no-xattrs -czf "$out/$ver/5050-cli_${v}_${t}.tar.gz" -C "$d" 5050 LICENSE
done
(cd "$out/$ver" && shasum -a 256 ./*.tar.gz | sed 's| \./| |' >checksums.txt)
"$out/.bin/signtool" sign -s "$src/e2e/keys/test.key" -m "$out/$ver/checksums.txt" -x "$out/$ver/checksums.txt.minisig"
echo "$ver" >"$out/latest.txt"
sed -n 2p "$src/e2e/keys/test.pub" >"$out/test.pub"
sed -n 2p "$src/e2e/keys/test-next.pub" >"$out/other.pub"
echo "Fixture release $ver in $out"
