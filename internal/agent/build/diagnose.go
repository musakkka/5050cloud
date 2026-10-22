// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"fmt"
	"regexp"
	"strings"
)

// The build rows of the failure diagnosis catalog (docs/SPEC.md 9.18).
// Each failed build carries exactly one sentence; the control plane shows it
// above the logs and in the CLI. Every rule has a captured log fixture in
// testdata/diagnose.

// diagContext is what the sentences need besides the log.
type diagContext struct {
	RootDir        string // shown as "/" when empty
	DockerfilePath string // "Dockerfile" when empty
	TimeoutSec     int
}

type diagRule struct {
	name string
	re   *regexp.Regexp
	// sentence builds the diagnosis from the first match.
	sentence func(m []string, c diagContext) string
}

// Rules run in order; the first match wins. Specific causes come before
// generic ones (disk full can surface as any other error).
var diagRules = []diagRule{
	{
		name: "disk_full",
		re:   regexp.MustCompile(`(?i)no space left on device`),
		sentence: func([]string, diagContext) string {
			return SentenceDiskFull
		},
	},
	{
		name: "railpack_nothing_detected",
		re:   regexp.MustCompile(`Railpack could not determine how to build the app`),
		sentence: func(_ []string, c diagContext) string {
			return fmt.Sprintf("Railpack could not detect a language in %s. Set the root directory or add a Dockerfile.", displayRoot(c.RootDir))
		},
	},
	{
		name: "npm_ci_lockfile",
		re:   regexp.MustCompile("(?i)`?npm ci`? command can only install with an existing package-lock\\.json"),
		sentence: func([]string, diagContext) string {
			return "npm ci needs a package-lock.json. Commit one or set a custom install command."
		},
	},
	{
		// yarn and pnpm: The engine "node" is incompatible with this module. Expected version ">=20". Got "18.20.4"
		name: "node_engine_yarn",
		re:   regexp.MustCompile(`The engine "node" is incompatible with this module\. Expected version "([^"]+)"`),
		sentence: func(m []string, _ diagContext) string {
			return nodeEngineSentence(m[1])
		},
	},
	{
		// pnpm: ERR_PNPM_UNSUPPORTED_ENGINE ... Expected version: >=20
		name: "node_engine_pnpm",
		re:   regexp.MustCompile(`ERR_PNPM_UNSUPPORTED_ENGINE.*?Expected version: ?([^\s]+(?: [^\s]+)?)`),
		sentence: func(m []string, _ diagContext) string {
			return nodeEngineSentence(m[1])
		},
	},
	{
		// npm with engine-strict: npm error notsup Required: {"node":">=20"}
		name: "node_engine_npm",
		re:   regexp.MustCompile(`(?:EBADENGINE|notsup).*?[Rr]equired: \{\s*"?node"?\s*:\s*['"]([^'"]+)['"]`),
		sentence: func(m []string, _ diagContext) string {
			return nodeEngineSentence(m[1])
		},
	},
	{
		name: "dockerfile_missing",
		re:   regexp.MustCompile(`(?i)failed to read dockerfile: open [^:]*: no such file or directory|failed to solve: failed to read dockerfile`),
		sentence: func(_ []string, c diagContext) string {
			return dockerfileMissingSentence(c.DockerfilePath)
		},
	},
	{
		name: "base_image_unauthorized",
		re:   regexp.MustCompile(`(?:failed to resolve source metadata for ([^\s:]+(?::[^\s:@]+)?(?:@sha256:[0-9a-f]+)?):[^\n]*(?:401 Unauthorized|403 Forbidden|unauthorized|denied|insufficient_scope)|load metadata for (\S+)\n#\d+ ERROR: [^\n]*(?:401 Unauthorized|403 Forbidden|unauthorized|denied|insufficient_scope|failed to authorize))`),
		sentence: func(m []string, _ diagContext) string {
			return unauthorizedSentence(firstNonEmpty(m[1], m[2]))
		},
	},
	{
		name: "base_image_not_found",
		re:   regexp.MustCompile(`failed to resolve source metadata for ([^\s:]+(?::[^\s:@]+)?(?:@sha256:[0-9a-f]+)?):[^\n]*not found`),
		sentence: func(m []string, _ diagContext) string {
			return notFoundSentence(m[1])
		},
	},
	{
		name: "no_start_command",
		re:   regexp.MustCompile(`No start command (?:detected|was found)`),
		sentence: func([]string, diagContext) string {
			return SentenceNoStartCommand
		},
	},
}

// Fixed sentences used by more than one code path.
const (
	SentenceDiskFull       = "The build host ran out of disk. Free space or add a data disk."
	SentenceNoStartCommand = "No start command was found. Set one in Settings > Deploy or add a start script."
	// SentenceGeneric is used when no rule matches. It still points at the logs.
	SentenceGeneric = "The build failed. The last lines of the build log show the error."
	sentenceAborted = "The build was cancelled."
)

// diagnose returns the catalog sentence for a failed build log, or
// SentenceGeneric when no rule matches. lines are the masked log lines.
func diagnose(lines []string, c diagContext) string {
	text := strings.Join(lines, "\n")
	for _, r := range diagRules {
		if m := r.re.FindStringSubmatch(text); m != nil {
			return r.sentence(m, c)
		}
	}
	return SentenceGeneric
}

// timeoutSentence is the build timeout row of 9.18.
func timeoutSentence(timeoutSec int) string {
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}
	if timeoutSec%60 == 0 {
		n := timeoutSec / 60
		unit := "minutes"
		if n == 1 {
			unit = "minute"
		}
		return fmt.Sprintf("The build took longer than %d %s and was stopped. Raise the build timeout in Settings > Build.", n, unit)
	}
	unit := "seconds"
	if timeoutSec == 1 {
		unit = "second"
	}
	return fmt.Sprintf("The build took longer than %d %s and was stopped. Raise the build timeout in Settings > Build.", timeoutSec, unit)
}

func dockerfileMissingSentence(path string) string {
	if path == "" {
		path = "Dockerfile"
	}
	return fmt.Sprintf("No Dockerfile at %s. Check the Dockerfile path in Settings > Build.", path)
}

func notFoundSentence(ref string) string {
	return fmt.Sprintf("Image %s was not found.", shortImageName(ref))
}

func unauthorizedSentence(ref string) string {
	return fmt.Sprintf("The registry refused access to %s. Add registry credentials in Settings > Source.", repositoryOf(shortImageName(ref)))
}

func nodeEngineSentence(req string) string {
	req = strings.TrimSpace(req)
	// ">=20" reads better as ">= 20", the form the spec uses.
	if m := regexp.MustCompile(`^([<>]=?|=)\s*(\S.*)$`).FindStringSubmatch(req); m != nil {
		req = m[1] + " " + m[2]
	}
	return fmt.Sprintf("The build needs Node %s (package.json engines). Set NODE_VERSION or update engines.", req)
}

func displayRoot(root string) string {
	root = strings.Trim(root, "/")
	if root == "" || root == "." {
		return "/"
	}
	return "/" + root
}

// shortImageName turns docker.io/library/node:22 into node:22 and
// docker.io/acme/app:1 into acme/app:1, the way people write them.
func shortImageName(ref string) string {
	ref = strings.TrimPrefix(ref, "index.docker.io/")
	ref = strings.TrimPrefix(ref, "docker.io/")
	ref = strings.TrimPrefix(ref, "library/")
	return ref
}

// repositoryOf drops the tag and digest from a reference.
func repositoryOf(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		ref = ref[:i]
	}
	return ref
}
