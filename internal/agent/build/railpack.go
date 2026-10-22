// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Railpack builds (docs/SPEC.md 8.2 step 4).

type railpackOutput struct {
	Provider     string
	IsNode       bool
	StartCommand string
	Digest       string
}

// railpackPlanFile is the part of plan.json the builder reads.
type railpackPlanFile struct {
	Secrets []string `json:"secrets"`
	Deploy  struct {
		StartCommand string `json:"startCommand"`
	} `json:"deploy"`
}

// railpackInfoFile is the part of info.json the builder reads.
type railpackInfoFile struct {
	Success           bool     `json:"success"`
	DetectedProviders []string `json:"detectedProviders"`
	Metadata          struct {
		Providers string `json:"providers"`
	} `json:"metadata"`
}

// railpackPrepareArgv builds `railpack prepare` with names only on argv.
// The child environment carries the values; Railpack v0.40 reads a bare
// `--env NAME` from its environment (core/app/environment.go FromEnvs).
func railpackPrepareArgv(p buildPlan, contextDir, planPath, infoPath string) (argv, env, reserved []string) {
	names, reserved := usableEnv(p.Env)
	argv = []string{"prepare", contextDir, "--plan-out", planPath, "--info-out", infoPath}
	if p.BuildCommand != "" {
		argv = append(argv, "--build-cmd", p.BuildCommand)
	}
	if p.StartCommand != "" {
		argv = append(argv, "--start-cmd", p.StartCommand)
	}
	for _, n := range names {
		if p.Env[n] == "" {
			continue // Railpack skips empty values anyway
		}
		argv = append(argv, "--env", n)
		env = append(env, n+"="+p.Env[n])
	}
	return argv, env, reserved
}

func (b *builder) buildRailpack(ctx context.Context, job proto.BuildJob, reg proto.Registry, endpoint string, p buildPlan, contextDir, planDir string, t buildTarget, log func(string)) (railpackOutput, error) {
	var out railpackOutput
	bin, err := b.railpackBinary(ctx, p.Railpack, log)
	if err != nil {
		return out, asFailure(ctx, err)
	}
	planPath := filepath.Join(planDir, "plan.json")
	infoPath := filepath.Join(planDir, "info.json")
	defer func() { _ = os.Remove(planPath); _ = os.Remove(infoPath) }()

	argv, env, reserved := railpackPrepareArgv(p, contextDir, planPath, infoPath)
	if len(reserved) > 0 {
		log(fmt.Sprintf("Not passed to the build because the build tools use these names themselves: %s", strings.Join(reserved, ", ")))
	}
	log(fmt.Sprintf("Preparing the build with Railpack %s", p.Railpack.Version))
	home := filepath.Join(planDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return out, fail("The build directory could not be created: %v.", err)
	}
	childEnv := append([]string{
		"PATH=" + defaultPath(),
		"HOME=" + home,
		"TMPDIR=" + planDir,
		"NO_COLOR=1",
	}, env...)
	if err := b.run.Run(ctx, cmdSpec{Path: bin, Args: argv, Env: childEnv, Dir: planDir}, log); err != nil {
		return out, err
	}

	var plan railpackPlanFile
	if err := readJSONFile(planPath, &plan); err != nil {
		return out, fail("Railpack did not write a build plan: %v.", err)
	}
	var info railpackInfoFile
	if err := readJSONFile(infoPath, &info); err == nil {
		if len(info.DetectedProviders) > 0 {
			out.Provider = info.DetectedProviders[0]
		} else if info.Metadata.Providers != "" {
			out.Provider = strings.Split(info.Metadata.Providers, ",")[0]
		}
		for _, pr := range info.DetectedProviders {
			if pr == "node" {
				out.IsNode = true
			}
		}
	}
	if out.Provider == "" {
		out.Provider = "railpack"
	}
	out.StartCommand = plan.Deploy.StartCommand
	if out.Provider != "railpack" {
		log("Railpack detected " + out.Provider)
	}

	frontend := b.frontendRef(ctx, p.Railpack, reg, endpoint)
	argvB, envB, err := b.railpackBuildArgv(job, p, plan, frontend, planPath, contextDir, t)
	if err != nil {
		return out, err
	}
	log(fmt.Sprintf("Building with BuildKit for %s", t.Platform))
	spec := cmdSpec{Path: b.dockerCLI, Args: argvB, Env: append(b.baseEnv(), envB...), Dir: planDir}
	if err := b.run.Run(ctx, spec, log); err != nil {
		return out, err
	}
	out.Digest, err = readDigest(t.Meta)
	return out, err
}

// railpackBuildArgv is `docker buildx build -f plan.json` with the pinned
// frontend and one --secret per variable the plan lists.
func (b *builder) railpackBuildArgv(job proto.BuildJob, p buildPlan, plan railpackPlanFile, frontend, planPath, contextDir string, t buildTarget) ([]string, []string, error) {
	names, _ := usableEnv(p.Env)
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	var secrets []string
	for _, s := range plan.Secrets {
		if have[s] && p.Env[s] != "" {
			secrets = append(secrets, s)
		}
	}
	sort.Strings(secrets)
	hash, err := b.secretsHash(secrets, p.Env)
	if err != nil {
		return nil, nil, err
	}
	argv := buildxArgs(t)
	argv = append(argv,
		"-f", planPath,
		"--build-arg", "BUILDKIT_SYNTAX="+frontend,
		"--build-arg", "cache-key="+cacheKey(job.ServiceID),
		"--build-arg", "secrets-hash="+hash,
	)
	sargs, senv := secretArgs(secrets, p.Env)
	argv = append(argv, sargs...)
	argv = append(argv, contextDir)
	return argv, senv, nil
}

func cacheKey(serviceID string) string {
	var b strings.Builder
	for _, r := range serviceID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "service"
	}
	return b.String()
}

// secretsHash changes when a secret value changes, so Railpack's cached
// steps that use it rebuild. Railpack writes this value into a layer, so it
// is an HMAC with a per-host key: it reveals nothing about the values.
func (b *builder) secretsHash(names []string, env map[string]string) (string, error) {
	key, err := b.hostKey()
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	for _, n := range names {
		m.Write([]byte(n))
		m.Write([]byte{0})
		m.Write([]byte(env[n]))
		m.Write([]byte{0})
	}
	return hex.EncodeToString(m.Sum(nil)), nil
}

func (b *builder) hostKey() ([]byte, error) {
	p := filepath.Join(b.opts.DataDir, "build", "secrets-hash.key")
	if k, err := os.ReadFile(p); err == nil && len(k) == 32 {
		return k, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fail("The builder key could not be stored: %v.", err)
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, fail("The builder key could not be generated: %v.", err)
	}
	if err := writeFileAtomic(p, k, 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

// frontendRef is the Railpack frontend image for BUILDKIT_SYNTAX: the copy
// the installer mirrored into the internal registry when it is there, else
// the upstream image, always by digest.
func (b *builder) frontendRef(ctx context.Context, pin RailpackPin, reg proto.Registry, endpoint string) string {
	if pin.Version == CurrentRailpack().Version {
		if ref := b.images()["railpack-frontend"]; ref != "" {
			return rewriteRegistry(ref, reg.Address, endpoint)
		}
	}
	b.mu.Lock()
	cached, ok := b.frontends[pin.Version]
	b.mu.Unlock()
	if ok {
		return cached
	}
	upstream := fmt.Sprintf("%s:v%s@%s", RailpackFrontendRepo, pin.Version, pin.FrontendDigest)
	if reg.Address == "" {
		return upstream
	}
	mirror := fmt.Sprintf("%s/%s@%s", reg.Address, MirrorFrontendRepo, pin.FrontendDigest)
	opts := []name.Option{}
	if reg.Insecure {
		opts = append(opts, name.Insecure)
	}
	ref, err := name.ParseReference(mirror, opts...)
	if err != nil {
		return upstream
	}
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	auth := authn.FromConfig(authn.AuthConfig{Username: reg.Username, Password: reg.Password})
	if _, err := remote.Head(ref, remote.WithContext(hctx), remote.WithAuth(auth)); err != nil {
		return upstream
	}
	got := fmt.Sprintf("%s/%s@%s", endpoint, MirrorFrontendRepo, pin.FrontendDigest)
	b.mu.Lock()
	b.frontends[pin.Version] = got
	b.mu.Unlock()
	return got
}

func readJSONFile(p string, v any) error {
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("the file is empty")
	}
	return json.Unmarshal(data, v)
}
