// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/pkg/api"
)

// secOwner sets the install up and returns a signed-in owner client with one
// project, its environment id and one service id.
func secOwner(t *testing.T, e *env) (owner *client, eid, sid string) {
	t.Helper()
	tok, err := e.c.CreateSetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owner = e.client()
	var me api.Me
	owner.do("POST", "/api/v1/setup", api.SetupRequest{Token: tok, Email: "owner@example.com", Name: "Owner", Password: "correct horse battery"}, &me, 200)
	var prj api.Project
	owner.do("POST", "/api/v1/projects", api.CreateProjectRequest{Name: "shop"}, &prj, 200)
	eid = prj.Environments[0].ID
	var cs api.CreateServiceResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/services", api.CreateServiceRequest{Name: "api", DeployNow: true,
		Source: api.SourceConfig{Type: "image", Image: "nginx:1.27"}}, &cs, 200)
	return owner, eid, cs.Services[0].ID
}

// A custom domain must never be the console host or live under the base
// domain, or Caddy would hand the console name to a tenant container.
func TestCustomDomainRefusesInstallNames(t *testing.T) {
	e := newEnv(t)
	owner, eid, sid := secOwner(t, e)
	owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ConsoleDomain: ptr("console.example.net"), BaseDomain: ptr("apps.example.net")}, nil, 200)
	for _, name := range []string{"console.example.net", "CONSOLE.example.net.", "apps.example.net", "x.apps.example.net"} {
		owner.do("POST", "/api/v1/environments/"+eid+"/services/"+sid+"/domains", api.CreateDomainRequest{Kind: "custom", Domain: name}, nil, 422)
	}
	owner.do("POST", "/api/v1/environments/"+eid+"/services/"+sid+"/domains", api.CreateDomainRequest{Kind: "custom", Domain: "www.customer.test"}, nil, 200)
}

func ptr[T any](v T) *T { return &v }

// A reset link plus a mailbox must not skip the second factor.
func TestPasswordResetKeepsTOTP(t *testing.T) {
	e := newEnv(t)
	owner, _, _ := secOwner(t, e)
	var ts api.TOTPSetupResponse
	owner.do("POST", "/api/v1/me/2fa/setup", nil, &ts, 200)
	owner.do("POST", "/api/v1/me/2fa/enable", api.TOTPEnableRequest{Code: auth.TOTPCode(ts.Secret, time.Now())}, &api.TOTPEnableResponse{}, 200)
	owner.do("PUT", "/api/v1/settings/smtp", api.SMTPSettings{Host: "smtp.example.com", Port: 587, From: "noreply@example.com"}, nil, 200)

	anon := e.client()
	anon.do("POST", "/api/v1/auth/forgot", api.ForgotPasswordRequest{Email: "owner@example.com"}, nil, 204)
	m, ok := e.mail.Last("owner@example.com")
	if !ok {
		t.Fatal("no reset email")
	}
	rt := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(m.Text)
	if rt == nil {
		t.Fatalf("no token in %q", m.Text)
	}
	var lr api.LoginResponse
	anon.do("POST", "/api/v1/auth/reset", api.ResetPasswordRequest{Token: rt[1], Password: "a brand new password"}, &lr, 200)
	if !lr.NeedTOTP || lr.Ticket == "" || lr.User != nil {
		t.Fatalf("reset answered %+v, want a TOTP ticket and no user", lr)
	}
	anon.do("GET", "/api/v1/me", nil, nil, 401)
	var me api.Me
	anon.do("POST", "/api/v1/auth/login/totp", api.TOTPLoginRequest{Ticket: lr.Ticket, Code: auth.TOTPCode(ts.Secret, time.Now())}, &me, 200)
	anon.do("GET", "/api/v1/me", nil, nil, 200)
}

// services= and deployment= must stay inside the authorized environment.
func TestLogsStayInsideTheEnvironment(t *testing.T) {
	e := newEnv(t)
	owner, eid, sid := secOwner(t, e)
	var prj api.Project
	owner.do("POST", "/api/v1/projects", api.CreateProjectRequest{Name: "other"}, &prj, 200)
	eid2 := prj.Environments[0].ID
	var cs api.CreateServiceResponse
	owner.do("POST", "/api/v1/environments/"+eid2+"/services", api.CreateServiceRequest{Name: "victim", DeployNow: true,
		Source: api.SourceConfig{Type: "image", Image: "nginx:1.27"}}, &cs, 200)
	victim := cs.Services[0].ID
	if len(cs.Deployments) != 1 {
		t.Fatalf("%d deployments", len(cs.Deployments))
	}
	owner.do("GET", "/api/v1/logs?environment="+eid+"&services="+sid, nil, nil, 200)
	owner.do("GET", "/api/v1/logs?environment="+eid+"&services="+sid+","+victim, nil, nil, 404)
	owner.do("GET", "/api/v1/logs?environment="+eid+"&services="+victim, nil, nil, 404)
	owner.do("GET", "/api/v1/logs?environment="+eid+"&deployment="+cs.Deployments[0].ID, nil, nil, 404)
	if strings.Contains(eid, ",") {
		t.Fatal("unexpected id")
	}
}
