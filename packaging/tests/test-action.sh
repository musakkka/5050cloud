#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Runs packaging/action/run.sh with a stand-in 5050 binary that records its
# arguments, and checks the arguments and the action outputs.
set -u
# shellcheck source=packaging/tests/lib.sh
. "$(dirname "$0")/lib.sh"
RUN=$REPO/packaging/action/run.sh

W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
cat >"$W/5050" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >"$STUB_ARGS"
printf '%s\n' "$FIFTY_URL" "$FIFTY_TOKEN" "$FIFTY_CONFIG" >"$STUB_ENV"
case " $* " in
*" --detach "*) echo "Uploaded 1.0 KB" >&2; echo "Deployment dep_123 started. Follow it with \`5050 logs dep_123\`" ;;
*) echo "build line"; echo "Deployed: https://web.example.com" ;;
esac
exit "${STUB_EXIT:-0}"
EOF
chmod +x "$W/5050"

act() { # act NAME [VAR=value ...]: runs the action script in a clean env
	n=$1; shift
	: >"$W/$n.out"
	cd "$W" && env -i PATH="$PATH" HOME="$W" RUNNER_TEMP="$W/rt-$n" GITHUB_OUTPUT="$W/$n.out" \
		GITHUB_STEP_SUMMARY="$W/$n.summary" GITHUB_SHA=0123456789abcdef GITHUB_REPOSITORY=acme/app \
		FIFTY_BIN="$W/5050" STUB_ARGS="$W/$n.args" STUB_ENV="$W/$n.env" \
		FIFTY_URL=https://console.example.com FIFTY_TOKEN=5050_secret "$@" \
		bash "$RUN" >"$W/$n.log" 2>&1
}
has_arg() { grep -qxF -- "$2" "$W/$1.args"; }
args_are() { # args_are NAME expected-args-one-per-line
	printf '%s\n' "$2" >"$W/$1.want"
	diff -u "$W/$1.want" "$W/$1.args"
}

mkdir -p "$W/rt-default"
check "default run succeeds" act default INPUT_MESSAGE=hello
check "default args" args_are default "up
--yes
--ci
--message
hello"
check "url output" grep -qx "url=https://web.example.com" "$W/default.out"
check "empty deployment-id without detach" grep -qx "deployment-id=" "$W/default.out"
check "step summary" grep -q "Deployed: https://web.example.com" "$W/default.summary"
check "token is masked" grep -q "::add-mask::5050_secret" "$W/default.log"
check "token reaches the CLI as FIFTY_TOKEN" sh -c "sed -n 2p '$W/default.env' | grep -qx 5050_secret"
check "config lives under RUNNER_TEMP" sh -c "sed -n 3p '$W/default.env' | grep -q '^$W/rt-default/'"

check "all inputs" act full INPUT_PROJECT=shop INPUT_ENVIRONMENT=staging INPUT_SERVICE=web \
	INPUT_PATH=apps/web INPUT_PATH_AS_ROOT=true INPUT_MESSAGE="m 1" INPUT_ARGS="--no-gitignore"
check "all inputs args" args_are full "up
--yes
--project
shop
--environment
staging
--service
web
--ci
--path-as-root
--message
m 1
--no-gitignore
apps/web"

check "detach" act detach INPUT_DETACH=true INPUT_MESSAGE=x
check "detach passes --detach not --ci" sh -c "grep -qx -- --detach '$W/detach.args' && ! grep -qx -- --ci '$W/detach.args'"
check "detach deployment-id output" grep -qx "deployment-id=dep_123" "$W/detach.out"

check "new service" act new INPUT_NEW_SERVICE=true INPUT_SERVICE_NAME=api INPUT_MESSAGE=x
check "new service args" sh -c "grep -qx -- --new '$W/new.args' && grep -qx api '$W/new.args'"

if act badname INPUT_SERVICE_NAME=api; then bad "service-name without new-service is refused"; else ok "service-name without new-service is refused"; fi

check "default message from SHA when git has no commit" act msg
check "default message" has_arg msg "acme/app@0123456"

if act fail STUB_EXIT=1 INPUT_MESSAGE=x; then bad "CLI failure fails the step"; else ok "CLI failure fails the step"; fi
check "failure is annotated" grep -q "::error::5050 up failed with exit code 1" "$W/fail.log"

if act notoken FIFTY_TOKEN=; then bad "empty token is refused"; else ok "empty token is refused"; fi
if act nourl FIFTY_URL=; then bad "empty url is refused"; else ok "empty url is refused"; fi
if act badurl FIFTY_URL=console.example.com; then bad "url without scheme is refused"; else ok "url without scheme is refused"; fi

finish
