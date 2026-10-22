// SPDX-License-Identifier: MIT

package cli

import (
	"strings"
	"testing"
)

// railwayInvocations are Railway CLI invocations from the Railway CLI
// reference, mapped to what 5050 runs (golden: testdata/railway.golden).
var railwayInvocations = []string{
	"login",
	"login --browserless",
	"logout",
	"whoami",
	"init",
	"init --name my-app",
	"init -n my-app --workspace acme",
	"link",
	"link --project my-app --environment staging --service web",
	"link -p my-app -t team1",
	"unlink",
	"status",
	"list",
	"open",
	"up",
	"up --detach",
	"up -c",
	"up --service web",
	"up ./api --path-as-root",
	"up --no-gitignore --verbose",
	"redeploy -y",
	"restart --service web -y",
	"down -y",
	"deploy -t postgres",
	"deploy --template umami -v ADMIN=me",
	"add -d postgres",
	"add --database redis",
	"add --repo owner/repo",
	"add -i nginx:1.27",
	"add --service worker",
	"add --service web --variables PORT=8080",
	"logs",
	"logs --build",
	"logs -d",
	"logs --deployment",
	"logs --json -n 100",
	"logs --filter @level:error --since 1h",
	"logs --http --status 500",
	"variables",
	"variables --kv",
	"variables -k",
	"variables --set KEY=value",
	"variables --set A=1 --set B=2 --skip-deploys",
	"variables --set=A=1",
	"variables --set-from-stdin KEY",
	"variables --service api --json",
	"variable set KEY=value",
	"variable list -k",
	"variable delete KEY",
	"run npm start",
	"run -- npm run migrate",
	"shell",
	"shell --silent",
	"ssh",
	"ssh -s web ls -la",
	"ssh -- ls",
	"connect",
	"connect Postgres",
	"domain",
	"domain -p 3000",
	"domain example.com --port 8080",
	"environment",
	"environment staging",
	"environment new staging",
	"environment new pr-1 -d production",
	"environment delete staging",
	"service",
	"service web",
	"service status --all",
	"volume list",
	"volume add --mount-path /data",
	"volume add -m /data",
	"volume delete",
	"volume update --name x",
	"scale 3",
	"scale --us-west2=3",
	"completion zsh",
	"--version",
	"-V",
	"docs",
	"dev",
	"functions list",
	"starship",
	"upgrade",
	"deployment list",
	"bucket create",
}

func TestRailwayGolden(t *testing.T) {
	var b strings.Builder
	for _, inv := range railwayInvocations {
		out, msg := TranslateRailway(strings.Fields(inv))
		got := "5050 " + strings.Join(out, " ")
		if msg != "" {
			got = msg
		}
		b.WriteString("railway " + inv + "\n  -> " + got + "\n")
	}
	golden(t, "railway.golden", b.String())
}

// Every translated invocation must parse as a 5050 command line.
func TestRailwayTranslationsParse(t *testing.T) {
	for _, inv := range railwayInvocations {
		out, msg := TranslateRailway(strings.Fields(inv))
		if msg != "" {
			continue
		}
		a := &App{}
		root := a.Root()
		cmd, rest, err := root.Find(out)
		if err != nil {
			t.Errorf("%s: %v", inv, err)
			continue
		}
		if err := cmd.ParseFlags(rest); err != nil {
			t.Errorf("railway %s -> %v: %v", inv, out, err)
		}
	}
}
