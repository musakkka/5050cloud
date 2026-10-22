// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// Image imports (docs/SPEC.md 8.2 step 6): the source image for the target
// platform is copied by digest into the internal registry, so every host
// runs the same bytes and rollbacks survive upstream deletes.

var (
	upperInRepo = regexp.MustCompile(`[A-Z]`)
	platformRe  = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9_]+(/[a-z0-9]+)?$`)
)

// parseSourceImage validates and normalizes an image name the way people
// write it ("nginx", "nginx:1.27", "ghcr.io/acme/app@sha256:...").
// Repositories must be lowercase; tags may use any case.
func parseSourceImage(s string) (name.Reference, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fail("The image name is empty. Set an image such as nginx:1.27 in Settings > Source.")
	}
	repo := s
	if i := strings.Index(repo, "@"); i >= 0 {
		repo = repo[:i]
	}
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	// The registry host may have capitals (hosts are case-insensitive); the
	// path may not.
	path := repo
	if i := strings.Index(repo, "/"); i >= 0 && strings.ContainsAny(repo[:i], ".:") {
		path = repo[i+1:]
	}
	if upperInRepo.MatchString(path) {
		return nil, fail("The image name %s is not valid: repository names must be lowercase.", s)
	}
	ref, err := name.ParseReference(s, name.WithDefaultRegistry(name.DefaultRegistry))
	if err != nil {
		return nil, fail("The image name %s is not valid: %s.", s, strings.TrimSuffix(err.Error(), "."))
	}
	return ref, nil
}

func (b *builder) importImage(ctx context.Context, job proto.BuildJob, reg proto.Registry, log func(string), res *proto.BuildResult) error {
	src, err := parseSourceImage(job.SourceImage)
	if err != nil {
		return err
	}
	platform, err := v1.ParsePlatform(platformOf(job.Platform))
	if err != nil || !platformRe.MatchString(platformOf(job.Platform)) {
		return fail("The platform %s is not valid.", job.Platform)
	}
	dstOpts := []name.Option{}
	if reg.Insecure {
		dstOpts = append(dstOpts, name.Insecure)
	}
	dst, err := name.ParseReference(job.Image, dstOpts...)
	if err != nil {
		return fail("The target image name %s is not valid: %v.", job.Image, err)
	}

	srcAuth := authn.Anonymous
	if a := job.SourceImageAuth; a != nil && a.Username != "" && authMatches(a.Server, src.Context().RegistryStr()) {
		srcAuth = authn.FromConfig(authn.AuthConfig{Username: a.Username, Password: a.Password})
	}
	dstAuth := authn.Anonymous
	if reg.Username != "" {
		dstAuth = authn.FromConfig(authn.AuthConfig{Username: reg.Username, Password: reg.Password})
	}

	shown := shortImageName(job.SourceImage)
	log(fmt.Sprintf("Resolving %s for %s", shown, platform.String()))
	img, err := remote.Image(src, remote.WithContext(ctx), remote.WithAuth(srcAuth), remote.WithPlatform(*platform))
	if err != nil {
		return importError(ctx, err, job.SourceImage, platform)
	}
	digest, err := img.Digest()
	if err != nil {
		return importError(ctx, err, job.SourceImage, platform)
	}
	// An index without our platform resolves to the index itself; refuse it
	// rather than push an image the host cannot run.
	if cf, err := img.ConfigFile(); err == nil && cf.OS != "" && (cf.OS != platform.OS || cf.Architecture != platform.Architecture) {
		return fail("Image %s has no build for %s.", shown, platform.String())
	}
	log(fmt.Sprintf("Copying %s (%s) into the internal registry", shown, digest.String()))
	started := time.Now()
	if err := remote.Write(dst, img, remote.WithContext(ctx), remote.WithAuth(dstAuth)); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var te *transport.Error
		if errors.As(err, &te) && (te.StatusCode == http.StatusUnauthorized || te.StatusCode == http.StatusForbidden) {
			return fail("The internal registry refused the push credential of this host. Restart the agent to fetch a fresh one.")
		}
		if strings.Contains(err.Error(), "no space left on device") {
			return fail("%s", SentenceDiskFull)
		}
		return fail("The image could not be copied into the internal registry: %v.", err)
	}
	log(fmt.Sprintf("Copied in %.1f s", time.Since(started).Seconds()))
	res.Image = job.Image
	res.Digest = digest.String()
	res.Provider = "image"
	return nil
}

// authMatches reports whether a credential for server applies to registry.
func authMatches(server, registry string) bool {
	if server == "" {
		return true
	}
	norm := func(s string) string {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		s = strings.TrimSuffix(strings.SplitN(s, "/", 2)[0], "/")
		switch s {
		case "docker.io", "index.docker.io", "registry-1.docker.io":
			return name.DefaultRegistry
		}
		return strings.ToLower(s)
	}
	return norm(server) == norm(registry)
}

func importError(ctx context.Context, err error, src string, p *v1.Platform) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var te *transport.Error
	if errors.As(err, &te) {
		switch te.StatusCode {
		case http.StatusNotFound:
			return fail("%s", notFoundSentence(src))
		case http.StatusUnauthorized, http.StatusForbidden:
			return fail("%s", unauthorizedSentence(src))
		}
		for _, d := range te.Errors {
			switch d.Code {
			case transport.ManifestUnknownErrorCode, transport.NameUnknownErrorCode:
				return fail("%s", notFoundSentence(src))
			case transport.UnauthorizedErrorCode, transport.DeniedErrorCode:
				return fail("%s", unauthorizedSentence(src))
			}
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "no child with platform") {
		return fail("Image %s has no build for %s.", shortImageName(src), p.String())
	}
	return fail("Image %s could not be read from its registry: %s.", shortImageName(src), strings.TrimSuffix(msg, "."))
}
