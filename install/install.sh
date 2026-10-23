#!/bin/sh
# SPDX-License-Identifier: MIT
#
# 5050cloud installer (docs/SPEC.md section 4).
#
#   curl -fsSL https://5050cloud.com/install.sh | sudo sh
#   wget -qO- https://5050cloud.com/install.sh | sudo sh
#   curl -fsSL https://5050cloud.com/install.sh | sh -s -- --cli
#
# Piped, it asks nothing. Run from a terminal (sh install.sh) it walks you
# through the choices. Every choice is also a flag and an environment
# variable FIFTY_<FLAG> (for example --acme-email or FIFTY_ACME_EMAIL).
# Every step is safe to run again. Run with --help for the flags.

set -eu

# Release signing keys (minisign, base64). The first is the current key, a
# second one is the announced next key. internal/upgrade/release-keys.txt
# holds the same keys; e2e/scripts/release-key.sh writes both.
RELEASE_PUBKEYS="RWS5v2vaQNNARGmod7/0v3a8ZYZGhT95PWYNvc+dXNxe3/gWarQEiKiR"

REPO_RELEASES="https://github.com/musakkka/5050cloud/releases"
INSTALL_DIR=/opt/5050
ETC_DIR=/etc/5050
DATA_DIR=/var/lib/5050
UNIT_DIR=/etc/systemd/system
TCP_PROXY_RANGE="20000-29999"

# ------------------------------------------------------------------ output

if [ -t 1 ]; then
	B=$(printf '\033[1m'); R=$(printf '\033[0m'); RED=$(printf '\033[31m'); YEL=$(printf '\033[33m')
else
	B=""; R=""; RED=""; YEL=""
fi
say() { printf '%s\n' "$*"; }
step() { printf '%s==>%s %s\n' "$B" "$R" "$*"; }
warn() { printf '%swarning:%s %s\n' "$YEL" "$R" "$*" >&2; }
die() { printf '%serror:%s %s\n' "$RED" "$R" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
	cat <<'EOF'
Install 5050cloud on this server, join it to an install, or install the CLI.

  curl -fsSL https://5050cloud.com/install.sh | sudo sh [-s -- FLAGS]

Flags (each also an environment variable FIFTY_<FLAG>, for example FIFTY_DOMAIN):
  --version vX.Y.Z           release to install (default: latest)
  --archive PATH             install from a local release tarball (checksums.txt and
                             checksums.txt.minisig must sit next to it)
  --insecure-skip-verify     with --archive only: skip the signature check (test builds)
  --release-base URL         download releases from URL (needs --release-pubkey)
  --release-pubkey KEY       minisign public key for --release-base
  --acme-ca staging|production|URL   ACME directory (default production)
  --public-ip IP             public IPv4 address (default: detected)
  --domain example.com       base domain for service domains (needs a wildcard A record)
  --console-domain NAME      console address (default console-<random>.<base domain>)
  --skip-dns-check           with --domain: do not check that the names point at this server
  --acme-email EMAIL         email for Let's Encrypt notices
  --data-disk auto|none|/dev/X   blank disk to keep all state on (default auto)
  --no-swap                  do not add a 2 GB swap file
  --no-auto-updates          do not turn on unattended security updates
  --firewall                 drop inbound traffic except SSH, 80, 443 and WireGuard
  --join URL --token T       join an existing install as a worker host
  --roles app,ingress,data,build   roles of a joining host (default app)
  --ca-pin sha256:HEX        pin the console certificate when it uses an internal CA
  --recovery-key-file PATH   where the recovery key is written (default /etc/5050/recovery-key.txt)
  --restore s3://bucket/prefix --recovery-key K   rebuild a control host from a bucket
      --s3-endpoint URL --s3-region R --s3-access-key K --s3-secret-key S
  --cli                      install only the 5050 command line tool
  --uninstall [--purge] [--yes]   remove 5050cloud (--purge also deletes data)
  --force                    continue on an operating system that is not supported
  --yes                      answer yes to every question
  --help                     show this help
EOF
}

# ------------------------------------------------------------------ options

env_or() { v=""; eval "v=\${FIFTY_$1:-}"; printf '%s' "$v"; }

VERSION=$(env_or VERSION)
ARCHIVE=$(env_or ARCHIVE)
INSECURE=$(env_or INSECURE_SKIP_VERIFY)
RELEASE_BASE=$(env_or RELEASE_BASE)
RELEASE_PUBKEY=$(env_or RELEASE_PUBKEY)
ACME_CA=$(env_or ACME_CA)
PUBLIC_IP=$(env_or PUBLIC_IP)
DOMAIN=$(env_or DOMAIN)
CONSOLE_DOMAIN=$(env_or CONSOLE_DOMAIN)
SKIP_DNS_CHECK=$(env_or SKIP_DNS_CHECK)
ACME_EMAIL=$(env_or ACME_EMAIL)
DATA_DISK=$(env_or DATA_DISK)
NO_SWAP=$(env_or NO_SWAP)
NO_AUTO_UPDATES=$(env_or NO_AUTO_UPDATES)
FIREWALL=$(env_or FIREWALL)
JOIN=$(env_or JOIN)
TOKEN=$(env_or TOKEN)
ROLES=$(env_or ROLES)
CA_PIN=$(env_or CA_PIN)
RECOVERY_KEY_FILE=$(env_or RECOVERY_KEY_FILE)
RESTORE=$(env_or RESTORE)
RECOVERY_KEY=$(env_or RECOVERY_KEY)
S3_ENDPOINT=$(env_or S3_ENDPOINT)
S3_REGION=$(env_or S3_REGION)
S3_ACCESS_KEY=$(env_or S3_ACCESS_KEY)
S3_SECRET_KEY=$(env_or S3_SECRET_KEY)
CLI_ONLY=$(env_or CLI)
UNINSTALL=$(env_or UNINSTALL)
PURGE=$(env_or PURGE)
FORCE=$(env_or FORCE)
YES=$(env_or YES)
BINARIES_CHANGED=""

need_arg() { [ "$#" -ge 2 ] && [ -n "$2" ] || die "$1 needs a value."; }
while [ "$#" -gt 0 ]; do
	case "$1" in
	--version) need_arg "$@"; VERSION=$2; shift ;;
	--archive) need_arg "$@"; ARCHIVE=$2; shift ;;
	--insecure-skip-verify) INSECURE=1 ;;
	--release-base) need_arg "$@"; RELEASE_BASE=$2; shift ;;
	--release-pubkey) need_arg "$@"; RELEASE_PUBKEY=$2; shift ;;
	--acme-ca) need_arg "$@"; ACME_CA=$2; shift ;;
	--public-ip) need_arg "$@"; PUBLIC_IP=$2; shift ;;
	--domain) need_arg "$@"; DOMAIN=$2; shift ;;
	--console-domain) need_arg "$@"; CONSOLE_DOMAIN=$2; shift ;;
	--skip-dns-check) SKIP_DNS_CHECK=1 ;;
	--acme-email) need_arg "$@"; ACME_EMAIL=$2; shift ;;
	--data-disk) need_arg "$@"; DATA_DISK=$2; shift ;;
	--no-swap) NO_SWAP=1 ;;
	--no-auto-updates) NO_AUTO_UPDATES=1 ;;
	--firewall) FIREWALL=1 ;;
	--join) need_arg "$@"; JOIN=$2; shift ;;
	--token) need_arg "$@"; TOKEN=$2; shift ;;
	--roles) need_arg "$@"; ROLES=$2; shift ;;
	--ca-pin) need_arg "$@"; CA_PIN=$2; shift ;;
	--recovery-key-file) need_arg "$@"; RECOVERY_KEY_FILE=$2; shift ;;
	--restore) need_arg "$@"; RESTORE=$2; shift ;;
	--recovery-key) need_arg "$@"; RECOVERY_KEY=$2; shift ;;
	--s3-endpoint) need_arg "$@"; S3_ENDPOINT=$2; shift ;;
	--s3-region) need_arg "$@"; S3_REGION=$2; shift ;;
	--s3-access-key) need_arg "$@"; S3_ACCESS_KEY=$2; shift ;;
	--s3-secret-key) need_arg "$@"; S3_SECRET_KEY=$2; shift ;;
	--cli) CLI_ONLY=1 ;;
	--uninstall) UNINSTALL=1 ;;
	--purge) PURGE=1 ;;
	--force) FORCE=1 ;;
	--yes | -y) YES=1 ;;
	--help | -h) usage; exit 0 ;;
	*) die "unknown flag $1. Run with --help for the list." ;;
	esac
	shift
done

[ -n "$ACME_CA" ] || ACME_CA=production
[ -n "$DATA_DISK" ] || DATA_DISK=auto
[ -n "$RECOVERY_KEY_FILE" ] || RECOVERY_KEY_FILE=$ETC_DIR/recovery-key.txt
if [ -n "$RELEASE_BASE" ] && [ -z "$RELEASE_PUBKEY" ]; then
	die "--release-base needs --release-pubkey, so the release can be verified."
fi
if [ -n "$RELEASE_PUBKEY" ]; then
	RELEASE_PUBKEYS=$RELEASE_PUBKEY
fi
if [ -n "$INSECURE" ] && [ -z "$ARCHIVE" ]; then
	die "--insecure-skip-verify only works together with --archive."
fi
if [ -n "$JOIN" ] && [ -z "$TOKEN" ]; then
	die "--join needs --token. Create one in the console under Hosts > Add host."
fi
if [ -n "$RESTORE" ] && [ -z "$RECOVERY_KEY" ]; then
	die "--restore needs --recovery-key."
fi

# Guided setup only when a person runs the script from a terminal; piped
# into sh, stdin is the script itself and nothing is asked.
INTERACTIVE=""
if [ -t 0 ] && [ -t 1 ] && [ -z "$YES" ]; then
	INTERACTIVE=1
fi
ask() { # ask VAR "question" default
	if [ -z "$INTERACTIVE" ]; then return 0; fi
	printf '%s%s%s [%s]: ' "$B" "$2" "$R" "$3"
	read -r ans || ans=""
	[ -n "$ans" ] || ans=$3
	eval "$1=\$ans"
}
confirm() { # confirm "question" -> 0 for yes
	if [ -n "$YES" ]; then return 0; fi
	if [ -z "$INTERACTIVE" ]; then return 1; fi
	printf '%s%s%s [y/N]: ' "$B" "$1" "$R"
	read -r ans || ans=""
	case "$ans" in y | Y | yes | YES) return 0 ;; esac
	return 1
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# ------------------------------------------------------------------ download and verify

fetch() { # fetch URL FILE
	if have curl; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed to download releases."
	fi
}

latest_version() {
	if [ -n "$RELEASE_BASE" ]; then
		fetch "$RELEASE_BASE/latest.txt" "$TMP/latest.txt" || die "cannot read $RELEASE_BASE/latest.txt."
	else
		fetch "$REPO_RELEASES/latest/download/latest.txt" "$TMP/latest.txt" || die "cannot find the latest release. Pass --version vX.Y.Z."
	fi
	tr -d ' \r\n' <"$TMP/latest.txt"
}

sha256_of() {
	if have sha256sum; then sha256sum "$1" | cut -d' ' -f1
	else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

b64d() { base64 -d 2>/dev/null || base64 -D; }
hexof() { od -An -tx1 | tr -d ' \n'; }

# minisign_verify FILE SIGFILE: checks FILE against every key in
# RELEASE_PUBKEYS. Uses minisign when installed, else OpenSSL 3 (Ed25519).
minisign_verify() {
	file=$1; sig=$2
	[ -n "$RELEASE_PUBKEYS" ] || die "this installer has no release signing key. Pass --release-pubkey KEY."
	for key in $RELEASE_PUBKEYS; do
		if have minisign; then
			if minisign -Vq -P "$key" -m "$file" -x "$sig" >/dev/null 2>&1; then return 0; fi
			continue
		fi
		have openssl || die "verifying the release signature needs minisign or OpenSSL 3. Install one of them."
		printf '%s' "$key" | b64d >"$TMP/pk.bin"
		sed -n 2p "$sig" | b64d >"$TMP/sig.bin"
		[ "$(wc -c <"$TMP/pk.bin" | tr -d ' ')" = 42 ] || die "the release public key is not a minisign key."
		[ "$(wc -c <"$TMP/sig.bin" | tr -d ' ')" = 74 ] || die "$sig is not a minisign signature."
		kid=$(dd if="$TMP/pk.bin" bs=1 skip=2 count=8 2>/dev/null | hexof)
		sid=$(dd if="$TMP/sig.bin" bs=1 skip=2 count=8 2>/dev/null | hexof)
		[ "$kid" = "$sid" ] || continue
		alg=$(dd if="$TMP/sig.bin" bs=1 count=2 2>/dev/null)
		printf '\060\052\060\005\006\003\053\145\160\003\041\000' >"$TMP/pk.der"
		dd if="$TMP/pk.bin" bs=1 skip=10 count=32 2>/dev/null >>"$TMP/pk.der"
		dd if="$TMP/sig.bin" bs=1 skip=10 count=64 2>/dev/null >"$TMP/sig.raw"
		openssl pkey -pubin -inform DER -in "$TMP/pk.der" -out "$TMP/pk.pem" 2>/dev/null ||
			die "OpenSSL cannot read an Ed25519 key here. Install minisign (apt install minisign or brew install minisign) and run again."
		case "$alg" in
		ED) openssl dgst -blake2b512 -binary "$file" >"$TMP/msg.bin" ;;
		Ed) cp "$file" "$TMP/msg.bin" ;;
		*) die "$sig uses an unknown signature algorithm." ;;
		esac
		if openssl pkeyutl -verify -pubin -inkey "$TMP/pk.pem" -rawin -in "$TMP/msg.bin" -sigfile "$TMP/sig.raw" >/dev/null 2>&1; then
			return 0
		fi
	done
	return 1
}

# get_release NAME: puts NAME of the release into $TMP/NAME after checking
# the signed checksums. Sets VERSION.
get_release() {
	name_tpl=$1 # uses @V@ for the version without v
	if [ -n "$ARCHIVE" ]; then
		[ -f "$ARCHIVE" ] || die "$ARCHIVE does not exist."
		dir=$(dirname "$ARCHIVE")
		name=$(basename "$ARCHIVE")
		cp "$ARCHIVE" "$TMP/$name"
		if [ -f "$dir/checksums.txt" ] && [ -f "$dir/checksums.txt.minisig" ]; then
			cp "$dir/checksums.txt" "$dir/checksums.txt.minisig" "$TMP/"
		elif [ -n "$INSECURE" ]; then
			warn "installing $name without checking its signature (--insecure-skip-verify)."
			RELEASE_FILE=$TMP/$name
			[ -n "$VERSION" ] || VERSION=v$(printf '%s' "$name" | sed -n 's/^5050[a-z-]*_\([0-9][^_]*\)_.*/\1/p')
			return 0
		else
			die "checksums.txt and checksums.txt.minisig must sit next to $ARCHIVE."
		fi
		[ -n "$VERSION" ] || VERSION=v$(printf '%s' "$name" | sed -n 's/^5050[a-z-]*_\([0-9][^_]*\)_.*/\1/p')
	else
		[ -n "$VERSION" ] || VERSION=$(latest_version)
		case "$VERSION" in v*) ;; *) VERSION=v$VERSION ;; esac
		base=${RELEASE_BASE:-$REPO_RELEASES/download}
		base=${base%/}/$VERSION
		name=$(printf '%s' "$name_tpl" | sed "s/@V@/${VERSION#v}/")
		step "Downloading 5050cloud $VERSION"
		fetch "$base/checksums.txt" "$TMP/checksums.txt" || die "cannot download $base/checksums.txt. Does release $VERSION exist?"
		fetch "$base/checksums.txt.minisig" "$TMP/checksums.txt.minisig" || die "cannot download the release signature."
		fetch "$base/$name" "$TMP/$name" || die "cannot download $base/$name."
	fi
	minisign_verify "$TMP/checksums.txt" "$TMP/checksums.txt.minisig" ||
		die "the release signature does not match the release key, so nothing was installed."
	want=$(awk -v n="$name" '{f=$2; sub(/^\*/, "", f); if (f == n) print $1}' "$TMP/checksums.txt")
	[ -n "$want" ] || die "$name is not listed in the signed checksums."
	got=$(sha256_of "$TMP/$name")
	[ "$want" = "$got" ] || die "the SHA-256 of $name does not match the signed checksum, so nothing was installed."
	RELEASE_FILE=$TMP/$name
}

# ------------------------------------------------------------------ CLI only

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "this processor ($(uname -m)) is not supported; 5050cloud runs on amd64 and arm64." ;;
	esac
}

install_cli() {
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	case "$os" in linux | darwin) ;; *) die "the CLI installer supports Linux and macOS." ;; esac
	arch=$(detect_arch)
	get_release "5050-cli_@V@_${os}_${arch}.tar.gz"
	mkdir -p "$TMP/cli"
	tar -xzf "$RELEASE_FILE" -C "$TMP/cli"
	if [ "$(id -u)" = 0 ] || [ -w /usr/local/bin ]; then
		dest=/usr/local/bin
	else
		dest=$HOME/.local/bin
		mkdir -p "$dest"
	fi
	cp "$TMP/cli/5050" "$dest/5050.new" && chmod 0755 "$dest/5050.new" && mv "$dest/5050.new" "$dest/5050"
	say "Installed the 5050 CLI $VERSION to $dest/5050."
	case ":$PATH:" in *":$dest:"*) ;; *) say "Add $dest to your PATH to run it." ;; esac
	say "Next: 5050 login https://<your console>"
}

# ------------------------------------------------------------------ uninstall

uninstall() {
	[ "$(id -u)" = 0 ] || die "run the uninstaller as root (sudo)."
	if [ -n "$PURGE" ] && [ -z "$YES" ]; then
		confirm "Delete $DATA_DIR, every app container and volume? This cannot be undone." ||
			die "not purging. Pass --yes to confirm without a question."
	fi
	step "Stopping 5050cloud"
	for u in 5050d 5050-agent 5050-net-restore; do
		systemctl disable --now "$u.service" >/dev/null 2>&1 || true
		rm -f "$UNIT_DIR/$u.service"
		rm -rf "$UNIT_DIR/$u.service.d"
	done
	systemctl daemon-reload || true
	if have docker; then
		docker ps -aq --filter label=5050.infra 2>/dev/null | xargs -r docker rm -f >/dev/null 2>&1 || true
	fi
	rm -f /usr/local/bin/5050d /usr/local/bin/5050-agent /usr/local/bin/5050
	rm -rf "$INSTALL_DIR"
	if [ -n "$PURGE" ]; then
		step "Deleting data"
		if have docker; then
			for l in 5050.replica 5050.job; do
				docker ps -aq --filter "label=$l" 2>/dev/null | xargs -r docker rm -f >/dev/null 2>&1 || true
			done
			docker volume ls -q 2>/dev/null | grep '^5050-' | xargs -r docker volume rm >/dev/null 2>&1 || true
			docker network ls --format '{{.Name}}' 2>/dev/null | grep '^5050-' | xargs -r docker network rm >/dev/null 2>&1 || true
		fi
		rm -rf "$DATA_DIR" "$ETC_DIR" /etc/sysctl.d/90-5050.conf
		say "Removed 5050cloud and its data."
	else
		say "Removed 5050cloud. App containers, $DATA_DIR and $ETC_DIR were kept (use --purge to delete them)."
	fi
}

# ------------------------------------------------------------------ preflight

preflight() {
	[ "$(id -u)" = 0 ] || die "run the installer as root: curl -fsSL https://5050cloud.com/install.sh | sudo sh"
	[ "$(uname -s)" = Linux ] || die "the server installer runs on Linux. For the CLI use --cli."
	ARCH=$(detect_arch)
	[ -d /run/systemd/system ] || die "systemd is required."
	[ -r /etc/os-release ] || die "cannot read /etc/os-release."
	# Read os-release in a subshell: it defines VERSION, which would replace
	# the release version chosen with --version.
	osr() { (. /etc/os-release && eval "printf '%s' \"\${$1:-}\""); }
	OS_ID=$(osr ID)
	OS_CODENAME=$(osr VERSION_CODENAME)
	VERSION_ID=$(osr VERSION_ID)
	PRETTY_NAME=$(osr PRETTY_NAME)
	case "$OS_ID:${VERSION_ID:-}" in
	ubuntu:24.04) ;;
	ubuntu:22.04 | debian:12) warn "$PRETTY_NAME is not tested in this release; continuing." ;;
	*)
		if [ -n "$FORCE" ]; then warn "${PRETTY_NAME:-this OS} is not supported; continuing because of --force."
		else die "${PRETTY_NAME:-this OS} is not supported. 5050cloud v0.1 runs on Ubuntu 24.04 (pass --force to try anyway)."; fi
		;;
	esac
	have apt-get || die "apt-get is required."

	mem_kb=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
	MEM_MB=$((mem_kb / 1024))
	[ "$MEM_MB" -ge 900 ] || die "at least 1 GB of RAM is needed (found ${MEM_MB} MB)."
	[ "$MEM_MB" -ge 1900 ] || warn "${MEM_MB} MB of RAM is tight; 2 GB or more is recommended."
	free_kb=$(df -Pk / | awk 'NR==2 {print $4}')
	[ "$free_kb" -ge 10485760 ] || die "at least 10 GB of free disk space is needed on /."

	if [ -z "$JOIN" ]; then
		for p in 80 443; do
			holder=$(ss -Hltnp "sport = :$p" 2>/dev/null | head -n1 || true)
			if [ -n "$holder" ] && ! printf '%s' "$holder" | grep -q '5050\|caddy'; then
				die "port $p is in use: $holder. Stop that program and run the installer again."
			fi
		done
	fi

	# Time sync.
	if ! systemctl is-active --quiet systemd-timesyncd && ! systemctl is-active --quiet chrony && ! systemctl is-active --quiet chronyd; then
		step "Turning on time sync"
		systemctl enable --now systemd-timesyncd >/dev/null 2>&1 || timedatectl set-ntp true >/dev/null 2>&1 ||
			die "no time sync is running and systemd-timesyncd could not be started. Install chrony and run again."
	fi
	if have curl; then
		hdr=$(curl -fsSI --max-time 10 https://www.google.com 2>/dev/null | tr -d '\r' | awk -F': ' 'tolower($1)=="date" {print $2}' || true)
		if [ -n "$hdr" ]; then
			remote=$(date -u -d "$hdr" +%s 2>/dev/null || echo "")
			if [ -n "$remote" ]; then
				skew=$(($(date -u +%s) - remote))
				[ "$skew" -lt 0 ] && skew=$((-skew))
				[ "$skew" -le 2 ] || warn "the clock is ${skew}s off; time sync should fix it within a few minutes."
			fi
		fi
	fi

	# IPv4.
	if ! ip -4 route show default 2>/dev/null | grep -q default; then
		die "5050cloud v0.1 needs a public IPv4 address."
	fi

	# Host firewalls.
	if have ufw && ufw status 2>/dev/null | grep -q 'Status: active'; then
		[ -z "$FIREWALL" ] || die "--firewall cannot run next to an active ufw. Turn one of them off."
		warn "ufw is active. Allow 80/tcp, 443/tcp, 51820/udp and $TCP_PROXY_RANGE/tcp: ufw allow 80/tcp; ufw allow 443/tcp; ufw allow 51820/udp; ufw allow ${TCP_PROXY_RANGE%-*}:${TCP_PROXY_RANGE#*-}/tcp"
	fi
	if have firewall-cmd && firewall-cmd --state >/dev/null 2>&1; then
		[ -z "$FIREWALL" ] || die "--firewall cannot run next to an active firewalld. Turn one of them off."
		warn "firewalld is active. Open 80/tcp, 443/tcp, 51820/udp and $TCP_PROXY_RANGE/tcp."
	fi

	# Docker Hub anonymous pull quota.
	if have curl; then
		tok=$(curl -fsS --max-time 10 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:ratelimitpreview/test:pull" 2>/dev/null | sed -n 's/.*"token":"\([^"]*\)".*/\1/p' || true)
		if [ -n "$tok" ]; then
			left=$(curl -fsSI --max-time 10 -H "Authorization: Bearer $tok" https://registry-1.docker.io/v2/ratelimitpreview/test/manifests/latest 2>/dev/null |
				tr -d '\r' | awk -F': ' 'tolower($1)=="ratelimit-remaining" {split($2, a, ";"); print a[1]}' || true)
			if [ -n "$left" ]; then
				say "Docker Hub pulls left for this IP: $left"
				[ "$left" -ge 20 ] || warn "fewer than 20 Docker Hub pulls are left for this IP; builds may fail until the limit resets."
			fi
		fi
	fi
}

# ------------------------------------------------------------------ packages and system

install_packages() {
	step "Installing packages"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	apt-get install -y -qq ca-certificates curl git iptables openssl >/dev/null
	dv=$(docker version --format '{{.Server.Version}}' 2>/dev/null || true)
	if [ -n "$dv" ] && [ "${dv%%.*}" -ge 24 ] 2>/dev/null; then
		say "Docker $dv is already installed."
		return 0
	fi
	step "Installing Docker Engine"
	install -m 0755 -d /etc/apt/keyrings
	fetch "https://download.docker.com/linux/$OS_ID/gpg" /etc/apt/keyrings/docker.asc
	chmod a+r /etc/apt/keyrings/docker.asc
	printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
		"$(dpkg --print-architecture)" "$OS_ID" "$OS_CODENAME" >/etc/apt/sources.list.d/docker.list
	apt-get update -qq
	apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin >/dev/null
	DOCKER_FRESH=1
}

DATA_MOUNTED=""
setup_data_disk() {
	[ "$DATA_DISK" != none ] || return 0
	if findmnt -n /data >/dev/null 2>&1 && [ -d /data/5050 ]; then
		DATA_MOUNTED=1
		return 0
	fi
	disk=""
	if [ "$DATA_DISK" = auto ]; then
		count=0
		for d in $(lsblk -dn -o NAME,TYPE | awk '$2=="disk" {print $1}'); do
			dev=/dev/$d
			[ "$(lsblk -n "$dev" | wc -l)" -eq 1 ] || continue
			[ -z "$(wipefs -n "$dev" 2>/dev/null | tail -n +2)" ] || continue
			[ -z "$(lsblk -n -o MOUNTPOINT "$dev" | tr -d ' \n')" ] || continue
			size=$(lsblk -bdn -o SIZE "$dev")
			[ "$size" -ge 21474836480 ] || continue
			count=$((count + 1))
			disk=$dev
		done
		if [ "$count" -eq 0 ]; then return 0; fi
		if [ "$count" -gt 1 ]; then
			warn "there are $count blank disks; none was used. Pick one with --data-disk /dev/X."
			return 0
		fi
	else
		disk=$DATA_DISK
		[ -b "$disk" ] || die "$disk is not a block device."
		if [ -n "$(wipefs -n "$disk" 2>/dev/null | tail -n +2)" ] || [ "$(lsblk -n "$disk" | wc -l)" -gt 1 ]; then
			die "$disk is not blank (it has partitions or a filesystem). Nothing was changed."
		fi
	fi
	if [ -n "$(docker ps -aq 2>/dev/null || true)" ]; then
		warn "Docker already has containers, so its data stays where it is. Only 5050cloud state moves to $disk."
		KEEP_DOCKER_ROOT=1
	fi
	step "Formatting $disk as the data disk"
	mkfs.ext4 -q -L 5050data "$disk"
	uuid=$(blkid -s UUID -o value "$disk")
	mkdir -p /data
	grep -q "UUID=$uuid" /etc/fstab || printf 'UUID=%s /data ext4 noatime,nofail 0 2\n' "$uuid" >>/etc/fstab
	mount /data
	mkdir -p /data/5050 /data/docker "$DATA_DIR"
	grep -q "^/data/5050 $DATA_DIR " /etc/fstab || printf '/data/5050 %s none bind,nofail,x-systemd.requires-mounts-for=/data 0 0\n' "$DATA_DIR" >>/etc/fstab
	mount "$DATA_DIR"
	DATA_MOUNTED=1
}

configure_docker() {
	mkdir -p /etc/docker
	root=""
	if [ -n "$DATA_MOUNTED" ] && [ -z "${KEEP_DOCKER_ROOT:-}" ]; then root=/data/docker; fi
	f=/etc/docker/daemon.json
	before=$(cat "$f" 2>/dev/null || true)
	if [ -n "$before" ]; then
		have python3 || die "$f exists and python3 is needed to merge our settings into it."
		python3 - "$f" "$root" <<'PY'
import json, sys
path, root = sys.argv[1], sys.argv[2]
with open(path) as fh:
    cfg = json.load(fh)
cfg["live-restore"] = True
cfg["log-driver"] = "json-file"
cfg.setdefault("log-opts", {}).update({"max-size": "20m", "max-file": "3"})
cfg.setdefault("builder", {}).setdefault("gc", {}).update({"enabled": True, "defaultKeepStorage": "20GB"})
if root:
    cfg["data-root"] = root
with open(path, "w") as fh:
    json.dump(cfg, fh, indent=2)
    fh.write("\n")
PY
	else
		{
			printf '{\n  "live-restore": true,\n  "log-driver": "json-file",\n'
			printf '  "log-opts": {"max-size": "20m", "max-file": "3"},\n'
			printf '  "builder": {"gc": {"enabled": true, "defaultKeepStorage": "20GB"}}'
			[ -z "$root" ] || printf ',\n  "data-root": "%s"' "$root"
			printf '\n}\n'
		} >"$f"
	fi
	if [ "$(cat "$f")" != "$before" ] || [ -n "${DOCKER_FRESH:-}" ]; then
		systemctl enable docker >/dev/null 2>&1 || true
		systemctl restart docker
	fi
	systemctl is-active --quiet docker || die "Docker did not start. See: journalctl -u docker"
}

configure_system() {
	step "Tuning the kernel"
	conntrack=$((MEM_MB * 32768 / 1024))
	[ "$conntrack" -ge 131072 ] || conntrack=131072
	cat >/etc/sysctl.d/90-5050.conf <<EOF
# Written by the 5050cloud installer.
vm.overcommit_memory = 1
vm.swappiness = 10
net.core.somaxconn = 4096
net.ipv4.ip_forward = 1
fs.inotify.max_user_watches = 524288
net.netfilter.nf_conntrack_max = $conntrack
net.ipv4.ip_local_port_range = 10240 65000
net.ipv4.ip_local_reserved_ports = $TCP_PROXY_RANGE
EOF
	modprobe nf_conntrack 2>/dev/null || true
	sysctl -q -p /etc/sysctl.d/90-5050.conf >/dev/null 2>&1 || warn "some kernel settings could not be applied; see sysctl -p /etc/sysctl.d/90-5050.conf"

	if [ -z "$NO_SWAP" ] && [ "$MEM_MB" -lt 8192 ] && [ -z "$(swapon --noheadings 2>/dev/null)" ]; then
		step "Adding a 2 GB swap file"
		fallocate -l 2G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none
		chmod 600 /swapfile
		mkswap /swapfile >/dev/null
		swapon /swapfile
		grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
	fi

	if [ -z "$NO_AUTO_UPDATES" ]; then
		apt-get install -y -qq unattended-upgrades >/dev/null 2>&1 || true
		cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
		mkdir -p /etc/apt/apt.conf.d
		printf 'Unattended-Upgrade::Automatic-Reboot "false";\n' >/etc/apt/apt.conf.d/52-5050-no-reboot
	fi

	if [ -n "$FIREWALL" ]; then
		step "Adding firewall rules"
		have nft || apt-get install -y -qq nftables >/dev/null
		cat >/etc/nftables-5050.conf <<EOF
table inet fifty_filter {
	chain input {
		type filter hook input priority 0; policy drop;
		ct state established,related accept
		iifname "lo" accept
		ip protocol icmp accept
		ip6 nexthdr icmpv6 accept
		tcp dport { 22, 80, 443 } accept
		tcp dport $TCP_PROXY_RANGE accept
		udp dport 51820 accept
		iifname "wg5050" accept
		iifname "docker0" accept
		iifname "br-*" accept
	}
}
EOF
		nft list table inet fifty_filter >/dev/null 2>&1 && nft delete table inet fifty_filter
		nft -f /etc/nftables-5050.conf
		cat >"$UNIT_DIR/5050-firewall.service" <<'EOF'
[Unit]
Description=5050cloud host firewall
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f /etc/nftables-5050.conf

[Install]
WantedBy=multi-user.target
EOF
		systemctl daemon-reload
		systemctl enable 5050-firewall.service >/dev/null 2>&1
	fi
}

# ------------------------------------------------------------------ binaries and units

install_binaries() {
	get_release "5050_@V@_linux_${ARCH}.tar.gz"
	rel=$INSTALL_DIR/releases/$VERSION
	sum=$(sha256_of "$RELEASE_FILE")
	# A re-run that changes the binaries restarts the running services.
	[ "$(readlink "$INSTALL_DIR/current" 2>/dev/null || true)" = "$rel" ] || BINARIES_CHANGED=1
	if [ "$(cat "$rel/.sha256" 2>/dev/null || true)" != "$sum" ]; then
		BINARIES_CHANGED=1
		step "Unpacking $VERSION"
		mkdir -p "$INSTALL_DIR/releases"
		rm -rf "$rel.tmp"
		mkdir -p "$rel.tmp"
		tar -xzf "$RELEASE_FILE" -C "$rel.tmp"
		for b in 5050d 5050-agent 5050; do
			[ -x "$rel.tmp/$b" ] || die "the release has no $b binary."
		done
		echo "$sum" >"$rel.tmp/.sha256"
		rm -rf "$rel"
		mv "$rel.tmp" "$rel"
	fi
	for l in current agent; do
		ln -sfn "$rel" "$INSTALL_DIR/$l.new"
		mv -T "$INSTALL_DIR/$l.new" "$INSTALL_DIR/$l"
	done
	ln -sfn "$INSTALL_DIR/current/5050d" /usr/local/bin/5050d
	ln -sfn "$INSTALL_DIR/agent/5050-agent" /usr/local/bin/5050-agent
	ln -sfn "$INSTALL_DIR/current/5050" /usr/local/bin/5050

	mkdir -p "$ETC_DIR"
	chmod 0755 "$ETC_DIR"
	if [ -n "$RELEASE_BASE" ]; then
		umask 022
		printf '{\n  "releaseBase": "%s",\n  "publicKeys": ["%s"]\n}\n' "${RELEASE_BASE%/}" "$RELEASE_PUBKEY" >"$ETC_DIR/release.json"
	fi

	for u in 5050d 5050-agent 5050-net-restore; do
		src=$rel/systemd/$u.service
		[ -f "$src" ] || die "the release has no systemd unit $u.service."
		cp "$src" "$UNIT_DIR/$u.service"
		if [ -n "$DATA_MOUNTED" ]; then
			mkdir -p "$UNIT_DIR/$u.service.d"
			printf '[Unit]\nRequiresMountsFor=/data %s\n' "$DATA_DIR" >"$UNIT_DIR/$u.service.d/10-data.conf"
		fi
	done
	if [ -n "$DATA_MOUNTED" ] && [ -z "${KEEP_DOCKER_ROOT:-}" ]; then
		mkdir -p "$UNIT_DIR/docker.service.d"
		printf '[Unit]\nRequiresMountsFor=/data\n' >"$UNIT_DIR/docker.service.d/10-5050-data.conf"
	fi
	systemctl daemon-reload
	systemctl enable 5050-net-restore.service >/dev/null 2>&1 || true
}

# ------------------------------------------------------------------ control host

detect_public_ip() {
	[ -z "$PUBLIC_IP" ] || return 0
	ip=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -n1)
	case "$ip" in
	10.* | 192.168.* | 172.1[6-9].* | 172.2[0-9].* | 172.3[01].* | 100.6[4-9].* | 100.[7-9][0-9].* | 100.1[01][0-9].* | 100.12[0-7].* | 127.* | "") ip="" ;;
	esac
	if [ -z "$ip" ]; then
		ip=$(curl -fsS --max-time 5 https://checkip.amazonaws.com 2>/dev/null | tr -d ' \n' || true)
	fi
	if [ -z "$ip" ]; then
		ip=$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null | tr -d ' \n' || true)
	fi
	[ -n "$ip" ] || die "cannot detect the public IP address. Pass --public-ip."
	PUBLIC_IP=$ip
}

rand4() { LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c 4; }

# resolve_a NAME prints the IPv4 addresses NAME has on a public resolver, one
# per line (nothing when it has none or no lookup tool exists).
resolve_a() {
	if have dig; then
		dig +short +time=3 +tries=1 @1.1.1.1 A "$1" 2>/dev/null
	elif have nslookup; then
		nslookup -timeout=3 "$1" 1.1.1.1 2>/dev/null | sed -n '/^Name:/,$ s/^Address: *\([0-9.]*\)$/\1/p'
	elif have getent; then
		getent ahostsv4 "$1" 2>/dev/null | awk '{print $1}' | sort -u
	fi | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || true
}

# check_domain_dns makes sure the console name and a random name under the base
# domain (which proves the wildcard record) point at this server, before the
# certificate authority is asked. A wrong record would spend its failed-
# validation allowance and end in a timeout.
check_domain_dns() {
	[ -z "$SKIP_DNS_CHECK" ] || return 0
	case "$DOMAIN" in *.sslip.io | *.nip.io) return 0 ;; esac
	have dig || have nslookup || have getent || { warn "no DNS lookup tool found; not checking $DOMAIN."; return 0; }
	probe=x$(rand4).$DOMAIN
	bad=""
	for n in "$CONSOLE_DOMAIN" "$probe"; do
		if ! resolve_a "$n" | grep -qx "$PUBLIC_IP"; then bad="$bad $n"; fi
	done
	[ -n "$bad" ] || return 0
	warn "these names do not point at $PUBLIC_IP yet:$bad"
	warn "add these DNS records at your DNS provider, then run the installer again:"
	warn "  A   $CONSOLE_DOMAIN   $PUBLIC_IP"
	warn "  A   *.$DOMAIN   $PUBLIC_IP"
	if [ -n "$INTERACTIVE" ] && confirm "Continue anyway? Certificates will fail until the records exist."; then
		return 0
	fi
	die "DNS is not ready. Add the records above (or pass --skip-dns-check to install anyway)."
}

init_control() {
	detect_public_ip
	sslip=$(printf '%s' "$PUBLIC_IP" | tr . -).sslip.io
	if [ -f "$ETC_DIR/config.json" ]; then
		say "Already initialised ($ETC_DIR/config.json); keeping the existing configuration."
		CONSOLE_DOMAIN=$(sed -n 's/.*"consoleDomain": *"\([^"]*\)".*/\1/p' "$ETC_DIR/config.json")
		return 0
	fi
	if [ -z "$DOMAIN" ]; then
		ask DOMAIN "Base domain for your apps (needs a wildcard A record to $PUBLIC_IP; Enter keeps the free sslip.io name)" "$sslip"
	fi
	[ -n "$DOMAIN" ] || DOMAIN=$sslip
	[ -n "$CONSOLE_DOMAIN" ] || CONSOLE_DOMAIN=console-$(rand4).$DOMAIN
	if [ -n "$INTERACTIVE" ]; then
		ask CONSOLE_DOMAIN "Console address" "$CONSOLE_DOMAIN"
		[ -n "$ACME_EMAIL" ] || ask ACME_EMAIL "Email for certificate notices (optional)" ""
	fi
	check_domain_dns
	acme=$ACME_CA
	case "$acme" in
	production) acme=https://acme-v02.api.letsencrypt.org/directory ;;
	staging) acme=https://acme-staging-v02.api.letsencrypt.org/directory ;;
	https://*) ;;
	*) die "--acme-ca must be staging, production or an https URL." ;;
	esac

	mkdir -p "$DATA_DIR"
	umask 077
	set -- --config "$ETC_DIR/config.json" --data-dir "$DATA_DIR" \
		--public-ip "$PUBLIC_IP" --base-domain "$DOMAIN" --console-domain "$CONSOLE_DOMAIN" \
		--acme-ca "$acme" --recovery-key-file "$RECOVERY_KEY_FILE" \
		--setup-url-file "$ETC_DIR/setup-url.txt" --join-token-file "$ETC_DIR/local-join-token"
	[ -z "$ACME_EMAIL" ] || set -- "$@" --acme-email "$ACME_EMAIL"
	[ -z "$RELEASE_BASE" ] || set -- "$@" --release-base "${RELEASE_BASE%/}"
	step "Initialising the control plane"
	if [ -n "$RESTORE" ]; then
		[ -z "$S3_ENDPOINT" ] || export FIFTY_S3_ENDPOINT="$S3_ENDPOINT"
		[ -z "$S3_REGION" ] || export FIFTY_S3_REGION="$S3_REGION"
		[ -z "$S3_ACCESS_KEY" ] || export FIFTY_S3_ACCESS_KEY="$S3_ACCESS_KEY"
		[ -z "$S3_SECRET_KEY" ] || export FIFTY_S3_SECRET_KEY="$S3_SECRET_KEY"
		"$INSTALL_DIR/current/5050d" restore-state --from "$RESTORE" --recovery-key "$RECOVERY_KEY" "$@"
	else
		"$INSTALL_DIR/current/5050d" init "$@"
	fi
	umask 022
}

start_control() {
	step "Starting 5050cloud"
	if [ -n "$BINARIES_CHANGED" ] && systemctl is-active --quiet 5050d.service; then
		systemctl restart 5050d.service
	fi
	systemctl enable --now 5050d.service >/dev/null
	i=0
	until curl -fsS --max-time 2 http://127.0.0.1:5050/healthz >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 60 ] || die "5050d did not start. See: journalctl -u 5050d"
		sleep 1
	done
	if [ ! -f "$ETC_DIR/agent.json" ]; then
		tok=$(cat "$ETC_DIR/local-join-token")
		"$INSTALL_DIR/agent/5050-agent" join --url http://127.0.0.1:5050 --token "$tok" --public-ip "$PUBLIC_IP" >/dev/null
		rm -f "$ETC_DIR/local-join-token"
	fi
	if [ -n "$BINARIES_CHANGED" ] && systemctl is-active --quiet 5050-agent.service; then
		systemctl restart 5050-agent.service
	fi
	systemctl enable --now 5050-agent.service >/dev/null
}

wait_console() {
	step "Waiting for the console certificate (up to 120 s)"
	i=0
	while [ "$i" -lt 120 ]; do
		if curl -fsS --max-time 3 "https://$CONSOLE_DOMAIN/readyz" >/dev/null 2>&1; then
			return 0
		fi
		if [ "$ACME_CA" = staging ] && curl -fsSk --max-time 3 "https://$CONSOLE_DOMAIN/readyz" >/dev/null 2>&1; then
			return 0 # a Let's Encrypt staging certificate is the expected result
		fi
		i=$((i + 3))
		sleep 3
	done
	return 1
}

print_done() {
	setup=$(cat "$ETC_DIR/setup-url.txt" 2>/dev/null || true)
	# On a re-run the owner usually exists and the link no longer works.
	if [ -n "$setup" ] && curl -fsS --max-time 5 http://127.0.0.1:5050/api/v1/setup 2>/dev/null | grep -q '"needsSetup":false'; then
		setup=""
		rm -f "$ETC_DIR/setup-url.txt"
	fi
	say ""
	say "${B}5050cloud $VERSION is ready.${R}"
	say ""
	if [ -n "$setup" ]; then
		say "  Open this link to create your account (valid 24 hours, works once):"
		say "  $setup"
		say ""
	fi
	if [ -f "$RECOVERY_KEY_FILE" ]; then
		if [ -t 1 ]; then
			say "  Recovery key (decrypts your backups; store it somewhere safe, it is shown only here"
			say "  and in Settings > Backups to the owner; also saved to $RECOVERY_KEY_FILE, 0600;"
			say "  move it off this machine and delete the file):"
			say "  $(cat "$RECOVERY_KEY_FILE")"
		else
			say "  Recovery key saved to $RECOVERY_KEY_FILE (0600). Move it off this machine and delete the file."
		fi
		say ""
	fi
	say "  Next: deploy something from the console, or install the CLI on your laptop:"
	say "  curl -fsSL https://5050cloud.com/install.sh | sh -s -- --cli"
}

# ------------------------------------------------------------------ join

join_host() {
	if [ -f "$ETC_DIR/agent.json" ]; then
		say "This host already joined ($ETC_DIR/agent.json)."
	else
		detect_public_ip
		step "Joining $JOIN"
		set -- join --url "$JOIN" --token "$TOKEN" --public-ip "$PUBLIC_IP"
		[ -z "$CA_PIN" ] || set -- "$@" --ca-pin "$CA_PIN"
		if [ -n "$ROLES" ]; then
			if "$INSTALL_DIR/agent/5050-agent" join -h 2>&1 | grep -q -- '-roles'; then
				set -- "$@" --roles "$ROLES"
			else
				warn "this agent takes its roles from the join token; pick the roles in Hosts > Add host."
			fi
		fi
		out=$("$INSTALL_DIR/agent/5050-agent" "$@") || die "joining failed: $out"
		say "$out" | grep -v '^Start the agent' || true
		reg=$(printf '%s\n' "$out" | sed -n 's/^registry-address: //p')
		if [ -n "$reg" ] && have python3; then
			python3 - /etc/docker/daemon.json "$reg" <<'PY'
import json, sys
path, reg = sys.argv[1], sys.argv[2]
try:
    with open(path) as fh:
        cfg = json.load(fh)
except FileNotFoundError:
    cfg = {}
regs = cfg.setdefault("insecure-registries", [])
if reg not in regs:
    regs.append(reg)
    with open(path, "w") as fh:
        json.dump(cfg, fh, indent=2)
        fh.write("\n")
PY
			systemctl restart docker
		fi
	fi
	systemctl enable --now 5050-agent.service >/dev/null
	say ""
	say "${B}This host joined the install.${R} It shows up under Hosts in the console within a minute."
}

# ------------------------------------------------------------------ main

if [ -n "$CLI_ONLY" ]; then
	install_cli
	exit 0
fi
if [ -n "$UNINSTALL" ]; then
	uninstall
	exit 0
fi

preflight
install_packages
setup_data_disk
configure_docker
configure_system
install_binaries
if [ -n "$JOIN" ]; then
	join_host
	exit 0
fi
init_control
start_control
if ! wait_console; then
	warn "no certificate for $CONSOLE_DOMAIN within 120 s. The usual cause is that ports 80 and 443 are not reachable from the internet, or the domain does not point at $PUBLIC_IP."
	warn "open ports 80 and 443 (or use --domain with a wildcard A record to $PUBLIC_IP); the certificate is retried automatically."
fi
print_done
