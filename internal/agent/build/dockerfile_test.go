// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"reflect"
	"testing"
)

func TestParseDockerfileArgs(t *testing.T) {
	src := `# syntax=docker/dockerfile:1.7
ARG NODE_VERSION=22
FROM node:${NODE_VERSION}-alpine AS build
arg API_URL
ARG A B=2 \
    C
# ARG COMMENTED
ARG API_URL
ARG lower_case=1
ARG 9BAD
RUN --mount=type=secret,id=NPM_TOKEN,env=NPM_TOKEN npm ci
RUN --mount=type=cache,target=/root/.npm --mount=id=SENTRY_AUTH,type=secret npm run build
RUN --mount=type=secret,target=/run/secrets/STRIPE_KEY cat /run/secrets/STRIPE_KEY
RUN <<EOF
ARG INSIDE_HEREDOC
EOF
ARG AFTER_HEREDOC
`
	info := parseDockerfile(src)
	wantArgs := []string{"NODE_VERSION", "API_URL", "A", "B", "C", "lower_case", "AFTER_HEREDOC"}
	if !reflect.DeepEqual(info.Args, wantArgs) {
		t.Errorf("args = %v, want %v", info.Args, wantArgs)
	}
	wantSecrets := []string{"NPM_TOKEN", "SENTRY_AUTH", "STRIPE_KEY"}
	if !reflect.DeepEqual(info.Secrets, wantSecrets) {
		t.Errorf("secrets = %v, want %v", info.Secrets, wantSecrets)
	}
}

func TestParseDockerfileEscapeDirective(t *testing.T) {
	src := "# escape=`\nFROM mcr.microsoft.com/windows\nARG ONE `\n  TWO\n"
	info := parseDockerfile(src)
	if !reflect.DeepEqual(info.Args, []string{"ONE", "TWO"}) {
		t.Errorf("args = %v", info.Args)
	}
}

func TestSecretMountID(t *testing.T) {
	cases := map[string]string{
		"type=secret,id=X":                    "X",
		"id=Y,type=secret,required=true":      "Y",
		"type=secret,target=/run/secrets/ZED": "ZED",
		"type=cache,id=npm":                   "",
		"type=secret":                         "",
		`type=secret,id="QUOTED"`:             "QUOTED",
	}
	for in, want := range cases {
		if got := secretMountID(in); got != want {
			t.Errorf("secretMountID(%q) = %q, want %q", in, got, want)
		}
	}
}
