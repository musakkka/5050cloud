// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/configfile"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// Config as code (docs/SPEC.md 9.20). The builder reads the file with
// pkg/configfile, the shared parser the control plane also uses, and
// returns the overlay in BuildResult.ConfigFile. The control plane merges
// it (Config.MergeConfigFile); the builder applies the build fields itself
// so the build that reads the file also honours it.

// ConfigReader finds and parses the config-as-code file of a checkout.
// repoDir is the checkout, rootDir the service root directory relative to
// it, configPath the Config file path setting (relative to the repo root,
// empty to search rootDir). envName is the deployment's environment, used
// only for the notices. It returns nil, nil, nil when there is neither a
// config file nor a nixpacks.toml. The error, when not nil, is one
// sentence for the build log and Diagnosis.
type ConfigReader func(repoDir, rootDir, configPath, envName string) (*proto.ConfigOverlay, []string, error)

// ReadConfigFile is the builder's default ConfigReader. The checkout is
// opened as an os.Root, so a symlink in an untrusted repository can never
// make the agent read a file outside it.
func ReadConfigFile(repoDir, rootDir, configPath, envName string) (*proto.ConfigOverlay, []string, error) {
	root, err := os.OpenRoot(repoDir)
	if err != nil {
		return nil, nil, fail("The source checkout could not be opened: %v.", err)
	}
	defer func() { _ = root.Close() }()
	ov, err := configfile.Load(root.FS(), rootDir, configPath)
	if err != nil {
		return nil, nil, configError(err, configPath)
	}
	if ov == nil {
		return nil, nil, nil
	}
	return ov, configNotices(ov, envName), nil
}

// configError turns a configfile error into one sentence.
func configError(err error, configPath string) error {
	var ce *configfile.Error
	if errors.As(err, &ce) {
		msg := ce.Error()
		if strings.Contains(msg, "escapes from parent") || strings.Contains(msg, "path escapes") {
			return fail("The config file path %s points outside the repository. Check the config file path in Settings > Source.", configPath)
		}
		return fail("%s", sentence(unquoteFields(msg)))
	}
	if strings.Contains(err.Error(), "escapes from parent") || strings.Contains(err.Error(), "path escapes") {
		return fail("A config file in this repository is a link that points outside it, so it was not read.")
	}
	return fail("The config file could not be read: %s", sentence(err.Error()))
}

// quotedField matches a quoted field name in a configfile message.
var quotedField = regexp.MustCompile(`(unknown field|Did you mean) "([A-Za-z0-9_$.-]+)"`)

// unquoteFields writes field names the way docs/SPEC.md 9.18 shows them:
// "railway.json line 7: unknown field buildComand. Did you mean
// buildCommand?".
func unquoteFields(msg string) string {
	return quotedField.ReplaceAllString(msg, "$1 $2")
}

// configNotices are the lines for fields the file sets but 5050cloud
// ignores, computed with the same merge the control plane runs. The
// Nixpacks line is left out because the builder logs it itself.
func configNotices(ov *proto.ConfigOverlay, envName string) []string {
	r := configfile.Merge(api.ServiceConfig{}, ov, envName)
	var out []string
	for _, n := range r.Notices {
		if n == sentenceNixpacks {
			continue
		}
		out = append(out, n)
	}
	return out
}

// sentenceNixpacks is logged when builder NIXPACKS was asked for (9.20.2).
const sentenceNixpacks = "Nixpacks config detected, building with Railpack."

// sentence makes sure s ends like a sentence.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "the reason is unknown."
	}
	switch s[len(s)-1] {
	case '.', '?', '!':
		return s
	}
	return s + "."
}

// cleanRel turns a user path into a clean slash path relative to the repo
// root ("" for the root itself). A leading slash means the repo root.
func cleanRel(p string) string {
	p = path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	return strings.TrimPrefix(p, "/")
}

// readLimited reads at most limit bytes of a file.
func readLimited(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fail("the file is larger than %d bytes", limit)
	}
	return data, nil
}
