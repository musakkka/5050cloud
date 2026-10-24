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
# Colour and animation only on a terminal. NO_COLOR, TERM=dumb or --no-color
# turn both off, CI turns the animation off. Piped or logged output stays
# plain text. This block is kept identical in install/install.sh and
# install/install-cli.sh.
ESC=$(printf '\033')
B=""; R=""; DIM=""; PINK=""; GRN=""; RED=""; YEL=""; CYN=""
UI_COLOR=""; UI_ANIM=""; UI_UTF=""
OK_MARK="*"; BAD_MARK="x"; WARN_MARK="!"; ASK_MARK="?"; PTR="->"; BAR_ON="#"; BAR_OFF="-"
STEP_N=0; STEP_TOTAL=0; STEP_LABEL=""; STEP_SUB=""; STEP_T=0; SPIN_PID=""; STTY_SAVE=""
START_T=$(date +%s)

# shellcheck disable=SC2034
ui_init() {
	if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != dumb ]; then
		UI_COLOR=1
		B="${ESC}[1m"; R="${ESC}[0m"; DIM="${ESC}[38;5;245m"; PINK="${ESC}[38;5;205m"
		GRN="${ESC}[38;5;78m"; RED="${ESC}[38;5;203m"; YEL="${ESC}[38;5;221m"; CYN="${ESC}[38;5;117m"
		[ -n "${CI:-}" ] || UI_ANIM=1
		case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
		*UTF-8* | *utf-8* | *UTF8* | *utf8*)
			UI_UTF=1
			OK_MARK="✓"; BAD_MARK="✗"; WARN_MARK="!"; ASK_MARK="?"; PTR="❯"; BAR_ON="▰"; BAR_OFF="▱"
			;;
		esac
	fi
}
for a in "$@"; do [ "$a" != --no-color ] || NO_COLOR=1; done
ui_init

have() { command -v "$1" >/dev/null 2>&1; }
fmt_time() { printf '%d:%02d' $(($1 / 60)) $(($1 % 60)); }
rep() { # rep STRING N: STRING repeated N times
	rep_out=""; rep_i=0
	while [ "$rep_i" -lt "$2" ]; do rep_out="$rep_out$1"; rep_i=$((rep_i + 1)); done
	printf '%s' "$rep_out"
}
bar() { # bar DONE TOTAL: a ten cell progress bar
	bar_t=$2; [ "$bar_t" -gt 0 ] || bar_t=1
	bar_f=$(($1 * 10 / bar_t)); [ "$bar_f" -le 10 ] || bar_f=10
	printf '%s%s' "$(rep "$BAR_ON" "$bar_f")" "$(rep "$BAR_OFF" $((10 - bar_f)))"
}

# The spinner runs in the background and redraws one line (spinner, step
# number, progress bar, label, elapsed time). Anything that prints while it
# runs goes through ui_pause / ui_resume so lines never interleave.
spin_loop() {
	trap - HUP
	if [ -n "$UI_UTF" ]; then set -- ⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏; else set -- '|' '/' '-' "\\"; fi
	nf=$#; i=0; el=0
	cols=$(tput cols 2>/dev/null || echo 80)
	label=$STEP_LABEL; [ -z "$STEP_SUB" ] || label="$label - $STEP_SUB"
	maxl=$((cols - 34)); [ "$maxl" -ge 20 ] || maxl=20
	label=$(printf '%s' "$label" | cut -c1-"$maxl")
	pb=$(bar $((STEP_N - 1)) "$STEP_TOTAL")
	while :; do
		if [ $((i % 10)) -eq 0 ]; then el=$(($(date +%s) - START_T)); fi
		f=""
		eval "f=\${$((i % nf + 1))}"
		printf '\r%s[K  %s%s%s %s[%s/%s]%s %s%s%s %s %s%s%s' "$ESC" "$PINK" "$f" "$R" "$DIM" "$STEP_N" "$STEP_TOTAL" "$R" "$PINK" "$pb" "$R" "$label" "$DIM" "$(fmt_time "$el")" "$R"
		sleep 0.1 2>/dev/null || sleep 1
		i=$((i + 1))
	done
}
spin_start() {
	[ -n "$UI_ANIM" ] || return 0
	[ -z "$SPIN_PID" ] || return 0
	printf '%s[?25l' "$ESC"
	spin_loop &
	SPIN_PID=$!
}
spin_stop() {
	[ -n "$SPIN_PID" ] || return 0
	kill -HUP "$SPIN_PID" 2>/dev/null || true
	wait "$SPIN_PID" 2>/dev/null || true
	SPIN_PID=""
	printf '\r%s[K%s[?25h' "$ESC" "$ESC"
}
ui_pause() { spin_stop; }
ui_resume() { if [ -n "$STEP_LABEL" ]; then spin_start; fi; }

step_end() { # step_end ok|fail: closes the running step
	[ -n "$STEP_LABEL" ] || return 0
	spin_stop
	if [ -n "$UI_ANIM" ]; then
		t=$(($(date +%s) - STEP_T))
		if [ "$1" = ok ]; then mark="${GRN}${OK_MARK}${R}"; else mark="${RED}${BAD_MARK}${R}"; fi
		printf '  %s %s[%s/%s]%s %s %s%ss%s\n' "$mark" "$DIM" "$STEP_N" "$STEP_TOTAL" "$R" "$STEP_LABEL" "$DIM" "$t" "$R"
	fi
	STEP_LABEL=""; STEP_SUB=""
}
step() { # step "Label": finishes the running step and starts the next
	step_end ok
	STEP_N=$((STEP_N + 1)); STEP_LABEL=$1; STEP_SUB=""; STEP_T=$(date +%s)
	if [ -n "$UI_ANIM" ]; then
		spin_start
	else
		printf '%s==>%s %s[%s/%s]%s %s\n' "$B" "$R" "$DIM" "$STEP_N" "$STEP_TOTAL" "$R" "$1"
	fi
}
sub() { # sub "detail": names the part of the step that is running
	STEP_SUB=$1
	if [ -n "$UI_ANIM" ]; then spin_stop; spin_start; else printf '    %s\n' "$1"; fi
}
say() { ui_pause; printf '%s\n' "$*"; ui_resume; }
note() { ui_pause; printf '  %s%s%s\n' "$DIM" "$*" "$R"; ui_resume; }
warn() { ui_pause; printf '  %s%s warning:%s %s\n' "$YEL" "$WARN_MARK" "$R" "$*" >&2; ui_resume; }
die() {
	step_end fail
	spin_stop
	printf '  %s%s error:%s %s\n' "$RED" "$BAD_MARK" "$R" "$*" >&2
	printf '  %sIt is safe to run the installer again once this is fixed. Flags: --help. Docs: https://5050cloud.com/docs%s\n' "$DIM" "$R" >&2
	exit 1
}

banner() {
	if [ -z "$UI_COLOR" ]; then
		printf '5050cloud installer\n'
		return 0
	fi
	if [ -n "$UI_UTF" ]; then
		printf '\n'
		for l in 1 2 3; do
			case $l in
			1) a='┏━┓┏━┓┏━┓┏━┓'; b='┏━╸╻  ┏━┓╻ ╻╺┳┓'; c=205 ;;
			2) a='┗━┓┃┃┃┗━┓┃┃┃'; b='┃  ┃  ┃ ┃┃ ┃ ┃┃'; c=206 ;;
			*) a='┗━┛┗━┛┗━┛┗━┛'; b='┗━╸┗━╸┗━┛┗━┛╺┻┛'; c=207 ;;
			esac
			printf '  %s[38;5;%sm%s%s %s%s%s\n' "$ESC" "$c" "$a" "$R" "$B" "$b" "$R"
			[ -z "$UI_ANIM" ] || sleep 0.06
		done
	else
		printf '\n  %s5050cloud%s\n' "$B" "$R"
	fi
	printf '  %sopen-source cloud on your own servers%s\n\n' "$DIM" "$R"
}

box() { # box TITLE "label=value" ...: a framed summary
	box_title=$1; shift
	box_w=${#box_title}
	for l in "$@"; do
		v=${l#*=}
		[ $((10 + ${#v})) -le "$box_w" ] || box_w=$((10 + ${#v}))
	done
	if [ -n "$UI_UTF" ]; then tl='╭'; tr_='╮'; bl='╰'; br='╯'; hz='─'; vt='│'; else tl='+'; tr_='+'; bl='+'; br='+'; hz='-'; vt='|'; fi
	printf '  %s%s%s%s%s\n' "$PINK" "$tl" "$(rep "$hz" $((box_w + 2)))" "$tr_" "$R"
	printf '  %s%s%s %s%s%s%s %s%s%s\n' "$PINK" "$vt" "$R" "$B$GRN" "$box_title" "$R" "$(rep ' ' $((box_w - ${#box_title})))" "$PINK" "$vt" "$R"
	printf '  %s%s%s%s%s\n' "$PINK" "$vt" "$(rep ' ' $((box_w + 2)))" "$vt" "$R"
	for l in "$@"; do
		k=${l%%=*}; v=${l#*=}
		printf '  %s%s%s %s%-9s%s %s%s %s%s%s\n' "$PINK" "$vt" "$R" "$DIM" "$k" "$R" "$v" "$(rep ' ' $((box_w - 10 - ${#v})))" "$PINK" "$vt" "$R"
	done
	printf '  %s%s%s%s%s\n' "$PINK" "$bl" "$(rep "$hz" $((box_w + 2)))" "$br" "$R"
}

on_exit() {
	rc=$?
	spin_stop
	if [ -n "$STTY_SAVE" ]; then stty "$STTY_SAVE" 2>/dev/null || true; fi
	if [ -n "$UI_ANIM" ]; then printf '%s[?25h' "$ESC"; fi
	[ -z "${TMP:-}" ] || rm -rf "$TMP"
	if [ "$rc" -eq 130 ]; then printf '\n  %sCancelled. Every step is safe to run again, so just start the installer again.%s\n' "$DIM" "$R" >&2; fi
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
  --dry-run                  walk through the installer without changing anything
  --no-color                 plain text, no colour or animation (NO_COLOR=1 does the same)
  --help                     show this help

Run from a terminal, the installer asks a few questions. Piped (curl | sudo sh),
in CI or with --yes it asks nothing and uses the flags and defaults.
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
DRY_RUN=$(env_or DRY_RUN)
BINARIES_CHANGED=""

need_arg() { if [ "$#" -lt 2 ] || [ -z "$2" ]; then die "$1 needs a value."; fi; }
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
	--dry-run) DRY_RUN=1 ;;
	--no-color) ;;
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
WIZARD_DONE=""
if [ -t 0 ] && [ -t 1 ] && [ -z "$YES" ]; then
	INTERACTIVE=1
fi
tty_raw() { # single key reads; fails when stdin is not a real terminal
	STTY_SAVE=$(stty -g 2>/dev/null) || { STTY_SAVE=""; return 1; }
	stty -icanon -echo min 1 time 0 2>/dev/null || { STTY_SAVE=""; return 1; }
}
tty_restore() {
	if [ -n "$STTY_SAVE" ]; then stty "$STTY_SAVE" 2>/dev/null || true; fi
	STTY_SAVE=""
}
key() { # prints up, down, enter, quit, eof, num1..num9 or other
	k=$(dd bs=1 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')
	case "$k" in
	1b)
		stty min 0 time 1 2>/dev/null || true
		k2=$(dd bs=1 count=2 2>/dev/null | od -An -tx1 | tr -d ' \n')
		stty min 1 time 0 2>/dev/null || true
		case "$k2" in 5b41 | 4f41) echo up ;; 5b42 | 4f42) echo down ;; *) echo other ;; esac
		;;
	0a | 0d) echo enter ;;
	6b) echo up ;;
	6a) echo down ;;
	71 | 03) echo quit ;;
	3[1-9]) echo "num${k#3}" ;;
	"") echo eof ;;
	*) echo other ;;
	esac
}
choose() { # choose VAR "Question" option...: arrow keys or digits, Enter to pick; VAR gets the number
	c_var=$1; c_q=$2; shift 2
	c_n=$#; c_sel=1
	ui_pause
	printf '\n  %s%s%s %s%s%s\n' "$PINK" "$ASK_MARK" "$R" "$B" "$c_q" "$R"
	if ! tty_raw; then
		c_i=1
		for o in "$@"; do printf '    %s) %s\n' "$c_i" "$o"; c_i=$((c_i + 1)); done
		printf '  Pick a number [1]: '
		read -r c_ans || c_ans=""
		case "$c_ans" in [1-9]) if [ "$c_ans" -le "$c_n" ]; then c_sel=$c_ans; fi ;; esac
		eval "$c_var=\$c_sel"
		return 0
	fi
	printf '%s[?25l' "$ESC"
	c_first=1
	while :; do
		if [ -z "$c_first" ]; then printf '%s[%dA' "$ESC" "$c_n"; fi
		c_first=""
		c_i=1
		for o in "$@"; do
			if [ "$c_i" -eq "$c_sel" ]; then
				printf '\r%s[K    %s%s %s%s\n' "$ESC" "$PINK" "$PTR" "$o" "$R"
			else
				printf '\r%s[K      %s%s%s\n' "$ESC" "$DIM" "$o" "$R"
			fi
			c_i=$((c_i + 1))
		done
		c_k=$(key)
		case "$c_k" in
		up) if [ "$c_sel" -gt 1 ]; then c_sel=$((c_sel - 1)); fi ;;
		down) if [ "$c_sel" -lt "$c_n" ]; then c_sel=$((c_sel + 1)); fi ;;
		num*) if [ "${c_k#num}" -le "$c_n" ]; then c_sel=${c_k#num}; fi ;;
		enter | eof) break ;;
		quit) tty_restore; printf '%s[?25h' "$ESC"; exit 130 ;;
		esac
	done
	tty_restore
	printf '%s[?25h' "$ESC"
	eval "$c_var=\$c_sel"
	ui_resume
}
ask() { # ask VAR "question" default
	if [ -z "$INTERACTIVE" ]; then return 0; fi
	ui_pause
	printf '\n  %s%s%s %s%s%s' "$PINK" "$ASK_MARK" "$R" "$B" "$2" "$R"
	if [ -n "$3" ]; then printf ' %s(%s)%s' "$DIM" "$3" "$R"; fi
	printf '\n  %s%s%s ' "$PINK" "$PTR" "$R"
	read -r ans || ans=""
	[ -n "$ans" ] || ans=$3
	eval "$1=\$ans"
	ui_resume
}
# shellcheck disable=SC2034
yesno() { # yesno VAR "question" y|n: VAR becomes 1 for yes, empty for no
	if [ -z "$INTERACTIVE" ]; then return 0; fi
	ui_pause
	if [ "$3" = y ]; then yn_hint="Y/n"; else yn_hint="y/N"; fi
	printf '\n  %s%s%s %s%s%s %s[%s]%s\n  %s%s%s ' "$PINK" "$ASK_MARK" "$R" "$B" "$2" "$R" "$DIM" "$yn_hint" "$R" "$PINK" "$PTR" "$R"
	read -r ans || ans=""
	case "$ans" in
	y | Y | yes | YES | Yes) yn_v=1 ;;
	n | N | no | NO | No) yn_v="" ;;
	*) if [ "$3" = y ]; then yn_v=1; else yn_v=""; fi ;;
	esac
	eval "$1=\$yn_v"
	ui_resume
}
confirm() { # confirm "question" -> 0 for yes (default no)
	if [ -n "$YES" ]; then return 0; fi
	if [ -z "$INTERACTIVE" ]; then return 1; fi
	yes_v=""
	yesno yes_v "$1" n
	[ -n "$yes_v" ]
}

TMP=$(mktemp -d)

# run_logged DESCRIPTION CMD...: runs CMD with its output kept in a log; the
# log is shown when CMD fails (and always in plain mode, as before).
run_logged() {
	rl_what=$1; shift
	if "$@" >"$TMP/run.log" 2>&1; then
		if [ -z "$UI_ANIM" ] && [ -s "$TMP/run.log" ]; then cat "$TMP/run.log"; fi
		return 0
	fi
	ui_pause
	tail -n 20 "$TMP/run.log" >&2 || true
	die "$rl_what failed (output above)."
}

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
		sub "Downloading 5050cloud $VERSION"
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
	step "Downloading and verifying the 5050 CLI"
	get_release "5050-cli_@V@_${os}_${arch}.tar.gz"
	step "Installing the 5050 CLI"
	mkdir -p "$TMP/cli"
	tar -xzf "$RELEASE_FILE" -C "$TMP/cli"
	if [ "$(id -u)" = 0 ] || [ -w /usr/local/bin ]; then
		dest=/usr/local/bin
	else
		dest=$HOME/.local/bin
		mkdir -p "$dest"
	fi
	cp "$TMP/cli/5050" "$dest/5050.new" && chmod 0755 "$dest/5050.new" && mv "$dest/5050.new" "$dest/5050"
	step_end ok
	say ""
	box "Installed the 5050 CLI $VERSION" "Path=$dest/5050" "Took=$(fmt_time $(($(date +%s) - START_T)))" "Next=5050 login https://<your console>"
	case ":$PATH:" in *":$dest:"*) ;; *) warn "$dest is not on your PATH yet. Add it to run 5050 from anywhere." ;; esac
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
		step_end ok
		say ""
		box "5050cloud was removed" "Data=deleted" "Took=$(fmt_time $(($(date +%s) - START_T)))"
	else
		step_end ok
		say ""
		box "5050cloud was removed" "Kept=app containers, $DATA_DIR, $ETC_DIR" "Delete=run again with --purge" "Took=$(fmt_time $(($(date +%s) - START_T)))"
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
	# shellcheck disable=SC1091
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
		sub "Turning on time sync"
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
	sub "Installing base packages"
	export DEBIAN_FRONTEND=noninteractive
	run_logged "updating the package index" apt-get update -qq
	run_logged "installing base packages" apt-get install -y -qq ca-certificates curl git iptables openssl
	dv=$(docker version --format '{{.Server.Version}}' 2>/dev/null || true)
	if [ -n "$dv" ] && [ "${dv%%.*}" -ge 24 ] 2>/dev/null; then
		note "Docker $dv is already installed."
		return 0
	fi
	sub "Installing Docker Engine"
	install -m 0755 -d /etc/apt/keyrings
	fetch "https://download.docker.com/linux/$OS_ID/gpg" /etc/apt/keyrings/docker.asc
	chmod a+r /etc/apt/keyrings/docker.asc
	printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/%s %s stable\n' \
		"$(dpkg --print-architecture)" "$OS_ID" "$OS_CODENAME" >/etc/apt/sources.list.d/docker.list
	run_logged "updating the package index" apt-get update -qq
	run_logged "installing Docker Engine" apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin
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
	sub "Formatting $disk as the data disk"
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
	sub "Tuning the kernel"
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
		sub "Adding a 2 GB swap file"
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
		sub "Adding firewall rules"
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
		sub "Unpacking $VERSION"
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
		note "Already initialised ($ETC_DIR/config.json); keeping the existing configuration."
		CONSOLE_DOMAIN=$(sed -n 's/.*"consoleDomain": *"\([^"]*\)".*/\1/p' "$ETC_DIR/config.json")
		return 0
	fi
	if [ -z "$DOMAIN" ]; then
		ask DOMAIN "Base domain for your apps (wildcard A record to $PUBLIC_IP; Enter keeps the free sslip.io name)" "$sslip"
	fi
	[ -n "$DOMAIN" ] || DOMAIN=$sslip
	[ -n "$CONSOLE_DOMAIN" ] || CONSOLE_DOMAIN=console-$(rand4).$DOMAIN
	if [ -n "$INTERACTIVE" ] && [ -z "$WIZARD_DONE" ]; then
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
	sub "Initialising the control plane"
	if [ -n "$RESTORE" ]; then
		[ -z "$S3_ENDPOINT" ] || export FIFTY_S3_ENDPOINT="$S3_ENDPOINT"
		[ -z "$S3_REGION" ] || export FIFTY_S3_REGION="$S3_REGION"
		[ -z "$S3_ACCESS_KEY" ] || export FIFTY_S3_ACCESS_KEY="$S3_ACCESS_KEY"
		[ -z "$S3_SECRET_KEY" ] || export FIFTY_S3_SECRET_KEY="$S3_SECRET_KEY"
		run_logged "restoring the control host" "$INSTALL_DIR/current/5050d" restore-state --from "$RESTORE" --recovery-key "$RECOVERY_KEY" "$@"
	else
		run_logged "initialising the control plane" "$INSTALL_DIR/current/5050d" init "$@"
	fi
	umask 022
}

start_control() {
	sub "Starting the control plane"
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
	sub "Waiting for the console certificate (up to 120 s)"
	i=0
	while [ "$i" -lt 120 ]; do
		if curl -fsS --max-time 3 "https://$CONSOLE_DOMAIN/readyz" >/dev/null 2>&1; then
			return 0
		fi
		if [ "$ACME_CA" = staging ] && curl -fsSk --max-time 3 "https://$CONSOLE_DOMAIN/readyz" >/dev/null 2>&1; then
			return 0 # a Let's Encrypt staging certificate is the expected result
		fi
		i=$((i + 3))
		sub "Waiting for the console certificate (${i} s of 120)"
		sleep 3
	done
	return 1
}

print_done() {
	step_end ok
	setup=$(cat "$ETC_DIR/setup-url.txt" 2>/dev/null || true)
	# On a re-run the owner usually exists and the link no longer works.
	if [ -n "$setup" ] && curl -fsS --max-time 5 http://127.0.0.1:5050/api/v1/setup 2>/dev/null | grep -q '"needsSetup":false'; then
		setup=""
		rm -f "$ETC_DIR/setup-url.txt"
	fi
	if [ -f "$RECOVERY_KEY_FILE" ]; then
		if [ -t 1 ]; then recovery=$(cat "$RECOVERY_KEY_FILE"); else recovery=""; fi
	else
		recovery=""
	fi
	summary "$setup" "$recovery"
}

# summary SETUP_URL RECOVERY_KEY: the closing box and what to do next.
summary() {
	took=$(fmt_time $(($(date +%s) - START_T)))
	if [ -n "$1" ]; then login="Open the setup link below"; else login="Sign in with your owner account"; fi
	printf '\n'
	box "5050cloud $VERSION is ready." "Console=https://$CONSOLE_DOMAIN" "Login=$login" "Apps=*.${DOMAIN:-your domain}" "Took=$took" "Logs=journalctl -u 5050d -f"
	printf '\n'
	if [ -n "$1" ]; then
		printf '  %sOpen this link to create your account (valid 24 hours, works once):%s\n' "$B" "$R"
		printf '  %s%s%s\n\n' "$CYN" "$1" "$R"
	fi
	if [ -n "$2" ]; then
		printf '  %sRecovery key%s (decrypts your backups; store it somewhere safe, it is shown only here\n' "$B" "$R"
		printf '  and in Settings > Backups to the owner; also saved to %s, 0600;\n' "$RECOVERY_KEY_FILE"
		printf '  move it off this machine and delete the file):\n'
		printf '  %s%s%s\n\n' "$YEL" "$2" "$R"
	elif [ -f "$RECOVERY_KEY_FILE" ]; then
		printf '  Recovery key saved to %s (0600). Move it off this machine and delete the file.\n\n' "$RECOVERY_KEY_FILE"
	fi
	printf '  %sNext%s: deploy something from the console, or install the CLI on your laptop:\n' "$B" "$R"
	printf '  %scurl -fsSL https://5050cloud.com/install.sh | sh -s -- --cli%s\n\n' "$CYN" "$R"
}

# ------------------------------------------------------------------ join

join_host() {
	if [ -f "$ETC_DIR/agent.json" ]; then
		note "This host already joined ($ETC_DIR/agent.json)."
	else
		detect_public_ip
		sub "Joining $JOIN"
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
		printf '%s\n' "$out" | grep -v '^Start the agent' || true
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
	step_end ok
	printf '\n'
	box "This host joined the install." "Console=$JOIN" "Roles=${ROLES:-app}" "Took=$(fmt_time $(($(date +%s) - START_T)))"
	printf '\n  It shows up under Hosts in the console within a minute.\n\n'
}

# ------------------------------------------------------------------ wizard

need_root() {
	[ -z "$DRY_RUN" ] || return 0
	[ "$(id -u)" = 0 ] || die "run the installer as root: sudo sh install.sh"
}

wizard_server() {
	[ -n "$INTERACTIVE" ] || return 0
	if [ -f "$ETC_DIR/config.json" ]; then
		note "Found an existing installation. Its configuration is kept."
		return 0
	fi
	if [ -n "$DRY_RUN" ] && [ -z "$PUBLIC_IP" ]; then PUBLIC_IP=203.0.113.10; else detect_public_ip; fi
	sslip=$(printf '%s' "$PUBLIC_IP" | tr . -).sslip.io
	if [ -z "$DOMAIN" ]; then
		ask DOMAIN "Base domain for your apps (wildcard A record to $PUBLIC_IP; Enter keeps the free sslip.io name)" "$sslip"
	fi
	[ -n "$DOMAIN" ] || DOMAIN=$sslip
	[ -n "$CONSOLE_DOMAIN" ] || CONSOLE_DOMAIN=console-$(rand4).$DOMAIN
	ask CONSOLE_DOMAIN "Console address" "$CONSOLE_DOMAIN"
	[ -n "$ACME_EMAIL" ] || ask ACME_EMAIL "Email for certificate notices (optional)" ""
	if [ -z "$FIREWALL" ] && ! { have ufw && ufw status 2>/dev/null | grep -q 'Status: active'; }; then
		yesno FIREWALL "Block inbound traffic except SSH, web and WireGuard ports?" n
	fi
	WIZARD_DONE=1
	fw=off; [ -z "$FIREWALL" ] || fw=on
	printf '\n'
	box "Ready to install" "Console=https://$CONSOLE_DOMAIN" "Apps=*.$DOMAIN" "Server IP=$PUBLIC_IP" "Email=${ACME_EMAIL:-none}" "Firewall=$fw"
	go=""
	yesno go "Start the installation?" y
	if [ -z "$go" ]; then
		say ""
		say "Nothing was changed."
		exit 0
	fi
	printf '\n'
}

wizard_join() {
	[ -n "$INTERACTIVE" ] || return 0
	[ -n "$JOIN" ] || ask JOIN "Console URL of the install to join (for example https://console.example.com)" ""
	[ -n "$TOKEN" ] || ask TOKEN "Join token (console: Hosts > Add host)" ""
	[ -n "$JOIN" ] || die "a console URL is needed to join. Pass --join URL."
	[ -n "$TOKEN" ] || die "--join needs --token. Create one in the console under Hosts > Add host."
	printf '\n'
}

wizard_mode() {
	[ -n "$INTERACTIVE" ] || return 0
	if [ -n "$CLI_ONLY$UNINSTALL$JOIN$RESTORE$DOMAIN$ARCHIVE$RELEASE_BASE$VERSION" ]; then return 0; fi
	mode=1
	choose mode "What would you like to do?" \
		"Install 5050cloud on this server" \
		"Join this server to an existing install (worker host)" \
		"Install only the 5050 command line tool" \
		"Uninstall 5050cloud from this server"
	case "$mode" in
	2) need_root; wizard_join ;;
	3) CLI_ONLY=1 ;;
	4)
		UNINSTALL=1
		yesno PURGE "Also delete all data (apps, volumes, backups on this machine)?" n
		;;
	*) need_root ;;
	esac
}

# dry_run walks through the screens with made up values and changes nothing.
dry_run() {
	STEP_TOTAL=8
	VERSION=${VERSION:-v0.0.0-preview}
	[ -n "$PUBLIC_IP" ] || PUBLIC_IP=203.0.113.10
	[ -n "$DOMAIN" ] || DOMAIN=$(printf '%s' "$PUBLIC_IP" | tr . -).sslip.io
	[ -n "$CONSOLE_DOMAIN" ] || CONSOLE_DOMAIN=console-demo.$DOMAIN
	note "Dry run: nothing on this machine is changed."
	for ph in "Checking this server|Reading the operating system|Checking memory, disk and ports" \
		"Installing packages and Docker|Installing base packages|Installing Docker Engine" \
		"Preparing storage and Docker|Looking for a blank data disk|Writing the Docker settings" \
		"Tuning the system|Tuning the kernel|Adding a 2 GB swap file" \
		"Installing 5050cloud $VERSION|Downloading 5050cloud $VERSION|Unpacking $VERSION" \
		"Setting up the control plane|Initialising the control plane" \
		"Starting services|Starting the control plane" \
		"Waiting for the HTTPS certificate|Waiting for the console certificate (6 s of 120)"; do
		oldifs=$IFS; IFS='|'
		# shellcheck disable=SC2086
		set -- $ph
		IFS=$oldifs
		step "$1"; shift
		for d in "$@"; do sub "$d"; sleep 1; done
	done
	step_end ok
	summary "https://$CONSOLE_DOMAIN/setup?token=preview" "preview-recovery-key-not-real"
}

# ------------------------------------------------------------------ main

banner
wizard_mode
if [ -n "$UNINSTALL" ] && [ -z "$DRY_RUN" ]; then
	STEP_TOTAL=1; [ -z "$PURGE" ] || STEP_TOTAL=2
	uninstall
	exit 0
fi
if [ -n "$CLI_ONLY" ] && [ -z "$DRY_RUN" ]; then
	STEP_TOTAL=2
	install_cli
	exit 0
fi
need_root
if [ -n "$JOIN" ]; then wizard_join; elif [ -z "$RESTORE" ]; then wizard_server; fi
if [ -n "$DRY_RUN" ]; then
	dry_run
	exit 0
fi

if [ -n "$JOIN" ]; then STEP_TOTAL=6; else STEP_TOTAL=8; fi
step "Checking this server"
preflight
step "Installing packages and Docker"
install_packages
step "Preparing storage and Docker"
setup_data_disk
configure_docker
step "Tuning the system"
configure_system
step "Installing 5050cloud ${VERSION:-}"
install_binaries
if [ -n "$JOIN" ]; then
	join_host
	exit 0
fi
step "Setting up the control plane"
init_control
step "Starting services"
start_control
step "Waiting for the HTTPS certificate"
if ! wait_console; then
	warn "no certificate for $CONSOLE_DOMAIN within 120 s. The usual cause is that ports 80 and 443 are not reachable from the internet, or the domain does not point at $PUBLIC_IP."
	warn "open ports 80 and 443 (or use --domain with a wildcard A record to $PUBLIC_IP); the certificate is retried automatically."
fi
print_done
