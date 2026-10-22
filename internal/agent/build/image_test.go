// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/musakkka/5050cloud/pkg/proto"
)

// startRegistry runs an in-memory registry; with user set it requires
// basic auth.
func startRegistry(t *testing.T, user, pass string) string {
	t.Helper()
	h := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, `{"errors":[{"code":"UNAUTHORIZED","message":"auth required"}]}`, http.StatusUnauthorized)
				return
			}
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func platformImage(t *testing.T, os, arch string) v1.Image {
	t.Helper()
	img, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = os, arch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestImportImageFromIndex(t *testing.T) {
	src := startRegistry(t, "puller", "pull-pass")
	dst := startRegistry(t, "push", "push-pass")
	amd := platformImage(t, "linux", "amd64")
	arm := platformImage(t, "linux", "arm64")
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	ref, _ := name.ParseReference(src + "/acme/app:1.0")
	if err := remote.WriteIndex(ref, idx, remote.WithAuth(basic("puller", "pull-pass"))); err != nil {
		t.Fatal(err)
	}
	b := testBuilder(t, &fakeRunner{}, nil, nil)
	job := proto.BuildJob{
		BuildID: "dep_img", SourceImage: src + "/acme/app:1.0", Platform: "linux/arm64",
		SourceImageAuth: &proto.RegistryAuth{Server: "http://" + src, Username: "puller", Password: "pull-pass"},
		Image:           dst + "/svc_1:dep_img",
	}
	reg := proto.Registry{Address: dst, Username: "push", Password: "push-pass", Insecure: true}
	log, lines := collect()
	res := b.Build(context.Background(), job, reg, log)
	if !res.OK {
		t.Fatalf("import failed: %s\n%s", res.Diagnosis, strings.Join(lines(), "\n"))
	}
	want, _ := arm.Digest()
	if res.Digest != want.String() || res.Provider != "image" || res.Image != job.Image {
		t.Errorf("result = %+v, want digest %s", res, want)
	}
	got, err := remote.Image(mustRef(t, job.Image), remote.WithAuth(basic("push", "push-pass")))
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := got.Digest(); d != want {
		t.Errorf("pushed digest %s, want %s", d, want)
	}
	if strings.Contains(strings.Join(lines(), "\n"), "pull-pass") {
		t.Errorf("the source password is in the log")
	}

	// A platform the index lacks.
	job.BuildID, job.Platform = "dep_img2", "linux/s390x"
	res = b.Build(context.Background(), job, reg, nil)
	if res.OK || !strings.Contains(res.Diagnosis, "has no build for linux/s390x") {
		t.Errorf("diagnosis = %q", res.Diagnosis)
	}
}

func mustRef(t *testing.T, s string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func basic(u, p string) authn.Authenticator {
	return authn.FromConfig(authn.AuthConfig{Username: u, Password: p})
}

func TestImportImageErrors(t *testing.T) {
	src := startRegistry(t, "", "")
	locked := startRegistry(t, "user", "pass")
	dst := startRegistry(t, "push", "push-pass")
	single := platformImage(t, "linux", "amd64")
	if err := remote.Write(mustRef(t, src+"/single:1"), single); err != nil {
		t.Fatal(err)
	}
	b := testBuilder(t, &fakeRunner{}, nil, nil)
	reg := proto.Registry{Address: dst, Username: "push", Password: "push-pass", Insecure: true}
	cases := []struct {
		src, platform, want string
	}{
		{src + "/missing:1", "linux/amd64", "Image " + src + "/missing:1 was not found."},
		{locked + "/private/app:1", "linux/amd64", "The registry refused access to " + locked + "/private/app. Add registry credentials in Settings > Source."},
		{"Nginx:1.27", "linux/amd64", "The image name Nginx:1.27 is not valid: repository names must be lowercase."},
		{"", "linux/amd64", "The build needs exactly one source: a repository, an upload or an image."},
		{src + "/single:1", "linux/arm64", "Image " + src + "/single:1 has no build for linux/arm64."},
		{src + "/single:1", "not a platform/", "The platform not a platform/ is not valid."},
	}
	for i, c := range cases {
		job := proto.BuildJob{BuildID: "dep_e" + string(rune('a'+i)), SourceImage: c.src, Platform: c.platform, Image: dst + "/svc:x"}
		if c.src == "" {
			job.SourceImage = ""
		}
		res := b.Build(context.Background(), job, reg, nil)
		if res.OK || res.Diagnosis != c.want {
			t.Errorf("%s: diagnosis = %q, want %q", c.src, res.Diagnosis, c.want)
		}
	}
	// A wrong push credential.
	job := proto.BuildJob{BuildID: "dep_push", SourceImage: src + "/single:1", Platform: "linux/amd64", Image: dst + "/svc:x"}
	res := b.Build(context.Background(), job, proto.Registry{Address: dst, Username: "push", Password: "wrong", Insecure: true}, nil)
	if res.OK || !strings.Contains(res.Diagnosis, "refused the push credential") {
		t.Errorf("diagnosis = %q", res.Diagnosis)
	}
}

func TestParseSourceImage(t *testing.T) {
	ok := map[string]string{
		"nginx":                     "index.docker.io/library/nginx:latest",
		"nginx:1.27-Alpine":         "index.docker.io/library/nginx:1.27-Alpine",
		"Ghcr.io/acme/app:v1":       "Ghcr.io/acme/app:v1",
		"localhost:5000/team/app:1": "localhost:5000/team/app:1",
	}
	for in, want := range ok {
		ref, err := parseSourceImage(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if ref.Name() != want {
			t.Errorf("%s -> %s, want %s", in, ref.Name(), want)
		}
	}
	for _, bad := range []string{"acme/App:1", "ghcr.io/Acme/app", "nginx:bad tag", " "} {
		if _, err := parseSourceImage(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestAuthMatches(t *testing.T) {
	cases := []struct {
		server, registry string
		want             bool
	}{
		{"", "ghcr.io", true},
		{"https://index.docker.io/v1/", "index.docker.io", true},
		{"docker.io", "index.docker.io", true},
		{"ghcr.io", "ghcr.io", true},
		{"GHCR.io", "ghcr.io", true},
		{"ghcr.io", "quay.io", false},
	}
	for _, c := range cases {
		if got := authMatches(c.server, c.registry); got != c.want {
			t.Errorf("authMatches(%q, %q) = %v", c.server, c.registry, got)
		}
	}
}
