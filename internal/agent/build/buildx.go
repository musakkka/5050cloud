// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// buildTarget is where a buildx build pushes, as BuildKit sees it.
type buildTarget struct {
	Image    string // push ref with the build registry address
	Cache    string // cache ref, "" for none
	Platform string
	NoCache  bool
	Meta     string // --metadata-file path
}

// reservedEnv are names a build tool reads for itself. A service variable
// with one of these names is not handed to buildx or Railpack, because it
// would change how the tool runs instead of what it builds.
var reservedEnv = map[string]bool{
	"PATH": true, "HOME": true, "TMPDIR": true, "USER": true, "SHELL": true,
	"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "LD_AUDIT": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true, "all_proxy": true,
	"GODEBUG": true, "GOTRACEBACK": true,
}

func isReservedEnv(name string) bool {
	if reservedEnv[name] {
		return true
	}
	for _, p := range []string{"DOCKER_", "BUILDX_", "BUILDKIT_", "LD_"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// validEnvName is what an exec environment can carry.
func validEnvName(name string) bool {
	if name == "" || strings.ContainsAny(name, "=\x00") {
		return false
	}
	return argNameRe.MatchString(name)
}

// usableEnv returns the sorted names of variables that can be passed to a
// child process, and the names left out because they are reserved.
func usableEnv(env map[string]string) (names, reserved []string) {
	for k, v := range env {
		if !validEnvName(k) || strings.ContainsRune(v, 0) {
			continue
		}
		if isReservedEnv(k) {
			reserved = append(reserved, k)
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	sort.Strings(reserved)
	return names, reserved
}

// baseEnv is the environment every docker CLI call gets. Nothing else from
// the agent's own environment leaks into builds.
func (b *builder) baseEnv() []string {
	return []string{
		"PATH=" + defaultPath(),
		"HOME=" + filepath.Join(b.opts.DataDir, "build", "home"),
		"DOCKER_HOST=" + b.opts.DockerHost,
		"DOCKER_CONFIG=" + b.dockerConfigDir(),
		"BUILDX_NO_DEFAULT_ATTESTATIONS=1",
		"DOCKER_CLI_HINTS=false",
	}
}

func defaultPath() string {
	if p := os.Getenv("PATH"); p != "" {
		return p
	}
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

func (b *builder) dockerConfigDir() string {
	return filepath.Join(b.opts.DataDir, "build", "docker")
}

// writeDockerConfig stores the push credential for the build registry in
// the builder's own DOCKER_CONFIG (0600), so it is never on argv.
func (b *builder) writeDockerConfig(endpoint string, reg proto.Registry) error {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	dir := b.dockerConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail("The builder configuration directory %s could not be created: %v.", dir, err)
	}
	if err := os.MkdirAll(filepath.Join(b.opts.DataDir, "build", "home"), 0o700); err != nil {
		return fail("The builder home directory could not be created: %v.", err)
	}
	auths := map[string]map[string]string{}
	if reg.Username != "" {
		a := base64.StdEncoding.EncodeToString([]byte(reg.Username + ":" + reg.Password))
		auths[endpoint] = map[string]string{"auth": a}
		if reg.Address != endpoint {
			auths[reg.Address] = map[string]string{"auth": a}
		}
	}
	data, err := json.MarshalIndent(map[string]any{"auths": auths}, "", "  ")
	if err != nil {
		return fail("The registry credential could not be written: %v.", err)
	}
	return writeFileAtomic(filepath.Join(dir, "config.json"), data, 0o600)
}

func writeFileAtomic(p string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".*")
	if err != nil {
		return fail("%s could not be written: %v.", p, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fail("%s could not be written: %v.", p, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fail("%s could not be written: %v.", p, err)
	}
	if err := tmp.Close(); err != nil {
		return fail("%s could not be written: %v.", p, err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fail("%s could not be written: %v.", p, err)
	}
	return nil
}

// rewriteRegistry swaps the registry address of ref for the one BuildKit
// reaches from the build network. Refs on other registries stay as they are.
func rewriteRegistry(ref, from, to string) string {
	if ref == "" || from == "" || to == "" || from == to {
		return ref
	}
	if strings.HasPrefix(ref, from+"/") {
		return to + ref[len(from):]
	}
	return ref
}

// buildxArgs is the argv shared by Railpack and Dockerfile builds.
func buildxArgs(t buildTarget) []string {
	args := []string{
		"buildx", "build",
		"--builder", builderName,
		"--progress", "plain",
		"--platform", t.Platform,
		"--provenance=false",
		"--sbom=false",
		"--metadata-file", t.Meta,
		"-t", t.Image,
		"--output", "type=image,push=true",
	}
	if t.Cache != "" {
		if !t.NoCache {
			args = append(args, "--cache-from", "type=registry,ref="+t.Cache)
		}
		args = append(args, "--cache-to", "type=registry,ref="+t.Cache+",mode=max,ignore-error=true")
	}
	if t.NoCache {
		args = append(args, "--no-cache")
	}
	return args
}

// secretArgs passes each name as a BuildKit secret. The value is read from
// an alias variable, so a secret named PATH cannot change the CLI's PATH.
func secretArgs(names []string, env map[string]string) (args, childEnv []string) {
	for i, n := range names {
		alias := fmt.Sprintf("FIFTY_BUILD_SECRET_%d", i)
		args = append(args, "--secret", "id="+n+",env="+alias)
		childEnv = append(childEnv, alias+"="+env[n])
	}
	return args, childEnv
}

// buildDockerfile builds with the repository's Dockerfile.
func (b *builder) buildDockerfile(ctx context.Context, job proto.BuildJob, p buildPlan, contextDir string, t buildTarget, log func(string)) (string, error) {
	df := filepath.Join(contextDir, filepath.FromSlash(p.DockerfilePath))
	data, err := readLimited(df, 4<<20)
	if err != nil {
		return "", fail("%s", dockerfileMissingSentence(p.DockerfilePath))
	}
	info := parseDockerfile(string(data))
	argv, env := dockerfileArgv(info, p, df, contextDir, t, log)
	log(fmt.Sprintf("Building %s with BuildKit for %s", p.DockerfilePath, t.Platform))
	spec := cmdSpec{Path: b.dockerCLI, Args: argv, Env: append(b.baseEnv(), env...), Dir: contextDir}
	if err := b.run.Run(ctx, spec, log); err != nil {
		return "", err
	}
	return readDigest(t.Meta)
}

// dockerfileArgv builds the buildx argv and child environment for a
// Dockerfile build. Only names go on argv.
func dockerfileArgv(info dockerfileInfo, p buildPlan, dockerfile, contextDir string, t buildTarget, log func(string)) ([]string, []string) {
	names, reserved := usableEnv(p.Env)
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	argv := buildxArgs(t)
	argv = append(argv, "-f", dockerfile)
	var env, visible []string
	for _, a := range info.Args {
		if !have[a] {
			continue
		}
		argv = append(argv, "--build-arg", a)
		env = append(env, a+"="+p.Env[a])
		visible = append(visible, a)
	}
	var secrets []string
	for _, s := range info.Secrets {
		if have[s] {
			secrets = append(secrets, s)
		}
	}
	sargs, senv := secretArgs(secrets, p.Env)
	argv = append(argv, sargs...)
	env = append(env, senv...)
	argv = append(argv, contextDir)

	if len(visible) == 0 {
		log("No variables are visible at build time. Name a variable in an ARG line to use it in the Dockerfile.")
	} else {
		log(fmt.Sprintf("Variables visible at build time (named by ARG): %s", strings.Join(visible, ", ")))
	}
	if len(secrets) > 0 {
		log(fmt.Sprintf("Variables mounted as build secrets: %s", strings.Join(secrets, ", ")))
	}
	var skipped []string
	for _, a := range info.Args {
		for _, r := range reserved {
			if a == r {
				skipped = append(skipped, a)
			}
		}
	}
	if len(skipped) > 0 {
		log(fmt.Sprintf("Not passed as build arguments because the build tools use these names themselves: %s", strings.Join(skipped, ", ")))
	}
	return argv, env
}

// readDigest reads the pushed image digest from buildx's metadata file.
func readDigest(meta string) (string, error) {
	data, err := os.ReadFile(meta)
	if err != nil {
		return "", fail("The build finished but its result could not be read: %v.", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fail("The build finished but its result could not be read: %v.", err)
	}
	if d, ok := m["containerimage.digest"].(string); ok && d != "" {
		return d, nil
	}
	return "", fail("The build finished but did not report an image digest.")
}

// lookPath resolves a binary once; a missing one fails later with a sentence.
func lookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}
