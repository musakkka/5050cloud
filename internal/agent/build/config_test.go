// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const railwayJSON = `{
  "$schema": "https://railway.com/railway.schema.json",
  "build": {
    "builder": "NIXPACKS",
    "buildCommand": "npm run build"
  },
  "deploy": {
    "startCommand": "node server.js",
    "preDeployCommand": ["npm run migrate"],
    "healthcheckPath": "/health",
    "numReplicas": 2,
    "sleepApplication": true
  },
  "environments": {
    "production": {
      "deploy": { "startCommand": "node server.js --prod" }
    }
  }
}
`

func TestReadConfigFileRailwayJSON(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{"railway.json": railwayJSON})
	ov, notices, err := ReadConfigFile(repo, "", "", "production")
	if err != nil {
		t.Fatal(err)
	}
	if ov.Path != "railway.json" || ov.Format != "json" {
		t.Fatalf("overlay = %+v", ov)
	}
	if got := *ov.Config.Deploy.StartCommand; got != "node server.js" {
		t.Errorf("start = %q", got)
	}
	if ov.Lines["deploy.startCommand"] != 8 {
		t.Errorf("lines = %v", ov.Lines)
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "railway.json: sleepApplication is not supported in 5050cloud v0.1 and was ignored.") {
		t.Errorf("notices = %q", notices)
	}
	if strings.Contains(joined, sentenceNixpacks) {
		t.Errorf("the Nixpacks line is logged by the plan, not the notices: %q", notices)
	}
}

func TestReadConfigFileUnknownField(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{"railway.json": "{\n\"build\": {\n\"buildComand\": \"x\"\n}\n}"})
	_, _, err := ReadConfigFile(repo, "", "", "")
	want := "railway.json line 3: unknown field buildComand in build. Did you mean buildCommand?"
	if err == nil || (err.Error() != want && err.Error() != "railway.json line 3: unknown field buildComand. Did you mean buildCommand?") {
		t.Errorf("err = %v", err)
	}
}

func TestReadConfigFilePriority(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{
		"api/railway.toml": "[deploy]\nstartCommand = \"toml\"\n",
		"api/railway.json": `{"deploy": {"startCommand": "json"}}`,
		"railway.json":     `{"deploy": {"startCommand": "root"}}`,
	})
	ov, _, err := ReadConfigFile(repo, "api", "", "")
	if err != nil || ov.Path != "api/railway.json" || *ov.Config.Deploy.StartCommand != "json" {
		t.Fatalf("picked %+v %v", ov, err)
	}
	writeTree(t, repo, map[string]string{"api/5050.yaml": "deploy:\n  startCommand: ours\n"})
	ov, _, err = ReadConfigFile(repo, "api", "", "")
	if err != nil || ov.Path != "api/5050.yaml" {
		t.Errorf("5050.yaml should win: %+v %v", ov, err)
	}
	// An explicit path is relative to the repo root.
	ov, _, err = ReadConfigFile(repo, "api", "/railway.json", "")
	if err != nil || ov.Path != "railway.json" || *ov.Config.Deploy.StartCommand != "root" {
		t.Errorf("config path: %+v %v", ov, err)
	}
	_, _, err = ReadConfigFile(repo, "api", "missing.json", "")
	if err == nil || !strings.HasSuffix(err.Error(), ".") {
		t.Errorf("missing explicit file: %v", err)
	}
	ov, notices, err := ReadConfigFile(t.TempDir(), "", "", "")
	if ov != nil || notices != nil || err != nil {
		t.Errorf("no file: %v %v %v", ov, notices, err)
	}
}

func TestReadConfigFileNixpacksOnly(t *testing.T) {
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{"nixpacks.toml": "[start]\ncmd = \"bin/start\"\n[phases.install]\ncmds = [\"npm ci\"]\n"})
	ov, notices, err := ReadConfigFile(repo, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ov == nil || ov.Path != "" || ov.Nixpacks == nil || ov.Nixpacks.StartCmd != "bin/start" {
		t.Fatalf("overlay = %+v", ov)
	}
	if len(notices) != 1 || notices[0] != "nixpacks.toml: phases.install is not supported by Railpack and was ignored." {
		t.Errorf("notices = %q", notices)
	}
}

func TestReadConfigFileCannotEscape(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"railway.json": `{"deploy": {"startCommand": "outside"}}`})
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadConfigFile(repo, "", "link/railway.json", ""); err == nil {
		t.Errorf("an explicit path through a symlink out of the repo must fail")
	}
	// A candidate that is a link out of the repository is never read.
	if err := os.Symlink(filepath.Join(outside, "railway.json"), filepath.Join(repo, "railway.json")); err != nil {
		t.Fatal(err)
	}
	ov, _, err := ReadConfigFile(repo, "", "", "")
	if err == nil && ov != nil && ov.Config.Deploy != nil {
		t.Errorf("read a file outside the repository: %+v", ov)
	}
	if err != nil && !strings.HasSuffix(err.Error(), ".") {
		t.Errorf("error is not a sentence: %v", err)
	}
}

func TestUnquoteFields(t *testing.T) {
	in := `railway.json line 7: unknown field "buildComand" in build. Did you mean "buildCommand"?`
	want := `railway.json line 7: unknown field buildComand in build. Did you mean buildCommand?`
	if got := unquoteFields(in); got != want {
		t.Errorf("got %q", got)
	}
	if sentence("x") != "x." || sentence("x?") != "x?" || sentence("") == "" {
		t.Errorf("sentence")
	}
}

func FuzzReadConfigFile(f *testing.F) {
	f.Add("railway.json", railwayJSON)
	f.Add("railway.toml", "[deploy]\nstartCommand = \"x\"\n")
	f.Add("5050.yaml", "deploy:\n  startCommand: x\n")
	f.Fuzz(func(t *testing.T, name, src string) {
		switch name {
		case "railway.json", "railway.toml", "5050.yaml", "5050.json":
		default:
			name = "railway.json"
		}
		repo := t.TempDir()
		writeTree(t, repo, map[string]string{name: src})
		_, _, err := ReadConfigFile(repo, "", "", "production")
		if err != nil {
			msg := err.Error()
			if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "?") {
				t.Fatalf("error is not a sentence: %q", msg)
			}
		}
	})
}
