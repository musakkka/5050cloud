// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/musakkka/5050cloud/internal/cp/core"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/core/ids"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

type orchestrator = cpapi.Orchestrator

// body decodes a request of type T.
func body[T any](x *Ctx) (T, error) {
	var v T
	err := x.decode(&v)
	return v, err
}

// orchCall runs f against the orchestrator.
func orchCall[T any](x *Ctx, f func(o orchestrator, ctx context.Context) (T, error)) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	v, err := f(o, x.R.Context())
	if err != nil {
		return nil, err
	}
	return v, nil
}

func orchDo(x *Ctx, f func(o orchestrator, ctx context.Context) error) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	return nil, f(o, x.R.Context())
}

func (s *Server) table() map[string]handler {
	c := s.C
	ctxOf := func(x *Ctx) context.Context { return x.R.Context() }
	m := map[string]handler{}
	add := func(key string, h handler) { m[key] = h }

	// ---------------------------------------------------------------- 11.1
	add("GET /healthz", func(x *Ctx) (any, error) { return rawBody{"text/plain; charset=utf-8", []byte("ok\n")}, nil })
	add("GET /readyz", s.readyz)
	add("GET /version", func(x *Ctx) (any, error) {
		return api.VersionInfo{Version: version.Version, Commit: version.Commit, Date: version.Date}, nil
	})
	add("GET /setup", func(x *Ctx) (any, error) { return c.SetupStatus(ctxOf(x)) })
	add("POST /setup", func(x *Ctx) (any, error) {
		if !s.login.Allow("setup|" + x.A.IP) {
			return nil, fail.RateLimited("Too many attempts. Wait a few minutes and try again.")
		}
		req, err := body[api.SetupRequest](x)
		if err != nil {
			return nil, err
		}
		u, err := c.Setup(ctxOf(x), req)
		if err != nil {
			return nil, err
		}
		x.A = core.Actor{UserID: u.ID, Email: u.Email, Role: u.Role}
		if err := s.setSession(x.W, x.R, u.ID); err != nil {
			return nil, err
		}
		return c.Me(ctxOf(x), u.ID)
	})
	add("POST /auth/login", func(x *Ctx) (any, error) {
		req, err := body[api.LoginRequest](x)
		if err != nil {
			return nil, err
		}
		key := "login|" + x.A.IP + "|" + strings.ToLower(req.Email)
		if !s.login.Allow(key) {
			return nil, fail.RateLimited("Too many login attempts. Wait 15 minutes and try again.")
		}
		u, ticket, err := c.Login(ctxOf(x), req.Email, req.Password)
		if err != nil {
			return nil, err
		}
		x.Target = strings.ToLower(req.Email)
		if ticket != "" {
			return api.LoginResponse{NeedTOTP: true, Ticket: ticket}, nil
		}
		s.login.Reset(key)
		x.A = core.Actor{UserID: u.ID, Email: u.Email, Role: u.Role}
		if err := s.setSession(x.W, x.R, u.ID); err != nil {
			return nil, err
		}
		in, _ := c.Install(ctxOf(x))
		return api.LoginResponse{User: &u, Install: &in}, nil
	})
	add("POST /auth/login/totp", func(x *Ctx) (any, error) {
		if !s.login.Allow("totp|" + x.A.IP) {
			return nil, fail.RateLimited("Too many attempts. Wait 15 minutes and try again.")
		}
		req, err := body[api.TOTPLoginRequest](x)
		if err != nil {
			return nil, err
		}
		u, err := c.LoginTOTP(ctxOf(x), req.Ticket, req.Code)
		if err != nil {
			return nil, err
		}
		x.A = core.Actor{UserID: u.ID, Email: u.Email, Role: u.Role}
		if err := s.setSession(x.W, x.R, u.ID); err != nil {
			return nil, err
		}
		return c.Me(ctxOf(x), u.ID)
	})
	add("POST /auth/logout", func(x *Ctx) (any, error) {
		if x.A.SessionHash != "" {
			if err := c.EndSession(ctxOf(x), x.A.SessionHash); err != nil {
				return nil, err
			}
		}
		http.SetCookie(x.W, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !c.File().Insecure, SameSite: http.SameSiteLaxMode})
		return nil, nil
	})
	add("POST /auth/forgot", func(x *Ctx) (any, error) {
		req, err := body[api.ForgotPasswordRequest](x)
		if err != nil {
			return nil, err
		}
		if !s.forgot.Allow("forgot|" + x.A.IP) {
			return nil, fail.RateLimited("Too many reset requests. Wait an hour and try again.")
		}
		x.Target = strings.ToLower(req.Email)
		return nil, c.Forgot(ctxOf(x), req.Email)
	})
	add("POST /auth/reset", func(x *Ctx) (any, error) {
		req, err := body[api.ResetPasswordRequest](x)
		if err != nil {
			return nil, err
		}
		u, ticket, err := c.ResetPassword(ctxOf(x), req.Token, req.Password)
		if err != nil {
			return nil, err
		}
		if ticket != "" {
			// The account has a second factor: finish through /auth/login/totp.
			return api.LoginResponse{NeedTOTP: true, Ticket: ticket}, nil
		}
		x.A = core.Actor{UserID: u.ID, Email: u.Email, Role: u.Role}
		if err := s.setSession(x.W, x.R, u.ID); err != nil {
			return nil, err
		}
		me, err := c.Me(ctxOf(x), u.ID)
		if err != nil {
			return nil, err
		}
		return api.LoginResponse{User: &me.User, Install: &me.Install}, nil
	})
	add("POST /auth/verify-email", func(x *Ctx) (any, error) {
		req, err := body[api.VerifyEmailRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.VerifyEmail(ctxOf(x), req.Token)
	})
	add("GET /me", func(x *Ctx) (any, error) { return c.Me(ctxOf(x), x.A.UserID) })
	add("PATCH /me", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateMeRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateMe(ctxOf(x), x.A, req)
	})
	add("POST /me/2fa/setup", func(x *Ctx) (any, error) { return c.TOTPSetup(ctxOf(x), x.A) })
	add("POST /me/2fa/enable", func(x *Ctx) (any, error) {
		req, err := body[api.TOTPEnableRequest](x)
		if err != nil {
			return nil, err
		}
		return c.TOTPEnable(ctxOf(x), x.A, req.Code)
	})
	add("POST /me/2fa/disable", func(x *Ctx) (any, error) {
		req, err := body[api.TOTPDisableRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.TOTPDisable(ctxOf(x), x.A, req.Password, req.Code)
	})
	add("GET /me/notification-prefs", func(x *Ctx) (any, error) { return c.NotificationPrefs(ctxOf(x), x.A.UserID) })
	add("PUT /me/notification-prefs", func(x *Ctx) (any, error) {
		req, err := body[[]api.NotificationPref](x)
		if err != nil {
			return nil, err
		}
		return c.PutNotificationPrefs(ctxOf(x), x.A.UserID, req)
	})
	add("GET /me/notifications", func(x *Ctx) (any, error) { return c.Notifications(ctxOf(x), x.A.UserID, x.q("before")) })
	add("POST /me/notifications/read", func(x *Ctx) (any, error) {
		req, err := body[api.MarkNotificationsReadRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.MarkNotificationsRead(ctxOf(x), x.A.UserID, req)
	})
	add("POST /cli/auth/start", func(x *Ctx) (any, error) {
		if !s.forgot.Allow("cli|" + x.A.IP) {
			return nil, fail.RateLimited("Too many login attempts from this address. Wait and try again.")
		}
		return c.CLIAuthStart(ctxOf(x))
	})
	add("POST /cli/auth/approve", func(x *Ctx) (any, error) {
		req, err := body[api.CLIAuthApprove](x)
		if err != nil {
			return nil, err
		}
		return nil, c.CLIAuthApprove(ctxOf(x), x.A, req.Code)
	})
	add("POST /cli/auth/poll", func(x *Ctx) (any, error) {
		req, err := body[api.CLIAuthPoll](x)
		if err != nil {
			return nil, err
		}
		return c.CLIAuthPoll(ctxOf(x), req.PollToken)
	})
	add("GET /tokens", func(x *Ctx) (any, error) { return c.Tokens(ctxOf(x), x.A) })
	add("POST /tokens", func(x *Ctx) (any, error) {
		req, err := body[api.CreateTokenRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = req.Name
		return c.CreateToken(ctxOf(x), x.A, req)
	})
	add("DELETE /tokens/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteToken(ctxOf(x), x.A, x.p("id")) })
	add("GET /members", func(x *Ctx) (any, error) { return c.Members(ctxOf(x)) })
	add("GET /invites", func(x *Ctx) (any, error) { return c.Invites(ctxOf(x)) })
	add("POST /invites", func(x *Ctx) (any, error) {
		req, err := body[api.CreateInviteRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = string(req.Role)
		return c.CreateInvite(ctxOf(x), x.A, req)
	})
	add("DELETE /invites/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteInvite(ctxOf(x), x.p("id")) })
	add("GET /invites/{id}", func(x *Ctx) (any, error) { return c.InvitePreview(ctxOf(x), x.p("id")) })
	add("POST /invites/accept", func(x *Ctx) (any, error) {
		req, err := body[api.AcceptInviteRequest](x)
		if err != nil {
			return nil, err
		}
		u, err := c.AcceptInvite(ctxOf(x), req)
		if err != nil {
			return nil, err
		}
		x.A = core.Actor{UserID: u.ID, Email: u.Email, Role: u.Role}
		if err := s.setSession(x.W, x.R, u.ID); err != nil {
			return nil, err
		}
		return c.Me(ctxOf(x), u.ID)
	})
	add("PATCH /members/{id}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateMemberRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateMember(ctxOf(x), x.A, x.p("id"), req)
	})
	add("DELETE /members/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteMember(ctxOf(x), x.A, x.p("id")) })
	add("POST /members/{id}/transfer-ownership", func(x *Ctx) (any, error) {
		req, err := body[api.TransferOwnershipRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.TransferOwnership(ctxOf(x), x.A, x.p("id"), req)
	})
	add("GET /audit", func(x *Ctx) (any, error) { return c.AuditLog(ctxOf(x), x.q("before"), x.qint("limit", 100)) })

	// ---------------------------------------------------------------- 11.2
	add("GET /install", func(x *Ctx) (any, error) { return c.Install(ctxOf(x)) })
	add("PATCH /install", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateInstallRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateInstall(ctxOf(x), x.A, req)
	})
	add("GET /settings/backups", func(x *Ctx) (any, error) { return c.BackupSettingsMasked(ctxOf(x)) })
	add("PUT /settings/backups", func(x *Ctx) (any, error) {
		req, err := body[api.BackupSettings](x)
		if err != nil {
			return nil, err
		}
		return c.PutBackupSettings(ctxOf(x), req)
	})
	add("POST /settings/backups/test", func(x *Ctx) (any, error) {
		req, err := body[api.BackupSettings](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.TestResult, error) {
			return o.TestBucket(ctx, c.FillBackupSecrets(ctx, req)), nil
		})
	})
	add("GET /settings/smtp", func(x *Ctx) (any, error) { return c.SMTPSettingsMasked(ctxOf(x)) })
	add("PUT /settings/smtp", func(x *Ctx) (any, error) {
		req, err := body[api.SMTPSettings](x)
		if err != nil {
			return nil, err
		}
		return c.PutSMTPSettings(ctxOf(x), req)
	})
	add("POST /settings/smtp/test", func(x *Ctx) (any, error) {
		req, err := body[api.SMTPTestRequest](x)
		if err != nil {
			return nil, err
		}
		return c.TestSMTP(ctxOf(x), req.To), nil
	})
	add("GET /settings/alerts", func(x *Ctx) (any, error) { return c.Alerts(ctxOf(x)) })
	add("PUT /settings/alerts", func(x *Ctx) (any, error) {
		req, err := body[api.InstallNotifications](x)
		if err != nil {
			return nil, err
		}
		return c.PutAlerts(ctxOf(x), req)
	})
	add("POST /settings/recovery-key", func(x *Ctx) (any, error) {
		req, err := body[api.RecoveryKeyRequest](x)
		if err != nil {
			return nil, err
		}
		return c.RecoveryKey(ctxOf(x), x.A, req.Password)
	})
	add("GET /hosts", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) ([]api.Host, error) { return o.Hosts(ctx) })
	})
	add("POST /hosts/join-tokens", func(x *Ctx) (any, error) {
		req, err := body[api.CreateJoinTokenRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.JoinToken, error) { return o.CreateJoinToken(ctx, req) })
	})
	add("PATCH /hosts/{id}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateHostRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Host, error) { return o.UpdateHost(ctx, x.p("id"), req) })
	})
	add("POST /hosts/{id}/maintenance", func(x *Ctx) (any, error) {
		req, err := body[api.MaintenanceRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Host, error) {
			return o.SetMaintenance(ctx, x.p("id"), req.On, x.A.Label())
		})
	})
	add("POST /hosts/{id}/readopt", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Host, error) {
			return o.ReadoptHost(ctx, x.p("id"), x.A.Label())
		})
	})
	add("DELETE /hosts/{id}/conflicts", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Host, error) {
			return o.ClearHostConflicts(ctx, x.p("id"))
		})
	})
	add("DELETE /hosts/{id}", func(x *Ctx) (any, error) {
		return orchDo(x, func(o orchestrator, ctx context.Context) error {
			return o.RemoveHost(ctx, x.p("id"), x.q("force") == "1")
		})
	})
	add("GET /hosts/{id}/metrics", func(x *Ctx) (any, error) {
		from, until := x.timeRange()
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.MetricsResponse, error) {
			return o.HostMetrics(ctx, x.p("id"), from, until)
		})
	})
	add("POST /upgrade", func(x *Ctx) (any, error) {
		req, err := body[api.UpgradeRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.UpgradeStatus, error) {
			return o.Upgrade(ctx, req, x.A.Label())
		})
	})
	add("GET /upgrade", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.UpgradeStatus, error) { return o.UpgradeStatus(ctx) })
	})
	add("GET /registry-credentials", func(x *Ctx) (any, error) { return c.RegistryCredentials(ctxOf(x)) })
	add("POST /registry-credentials", func(x *Ctx) (any, error) {
		req, err := body[api.CreateRegistryCredentialRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = req.Server
		return c.CreateRegistryCredential(ctxOf(x), req)
	})
	add("DELETE /registry-credentials/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteRegistryCredential(ctxOf(x), x.p("id")) })
	add("GET /restore/unknown-replicas", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) ([]api.UnknownReplica, error) { return o.UnknownReplicas(ctx) })
	})
	add("POST /restore/unknown-replicas/{id}/adopt", func(x *Ctx) (any, error) {
		return orchDo(x, func(o orchestrator, ctx context.Context) error { return o.AdoptReplica(ctx, x.p("id"), x.A.Label()) })
	})
	add("POST /restore/unknown-replicas/{id}/remove", func(x *Ctx) (any, error) {
		return orchDo(x, func(o orchestrator, ctx context.Context) error {
			return o.RemoveUnknownReplica(ctx, x.p("id"), x.A.Label())
		})
	})
	add("POST /restore/finish", func(x *Ctx) (any, error) {
		return orchDo(x, func(o orchestrator, ctx context.Context) error { return o.FinishRestore(ctx, x.A.Label()) })
	})

	// ---------------------------------------------------------------- 11.3
	add("GET /projects", func(x *Ctx) (any, error) { return c.Projects(ctxOf(x), x.A, x.q("sort")) })
	add("POST /projects", func(x *Ctx) (any, error) {
		req, err := body[api.CreateProjectRequest](x)
		if err != nil {
			return nil, err
		}
		p, err := c.CreateProject(ctxOf(x), x.A, req)
		x.Target = p.ID
		return p, err
	})
	add("GET /projects/{pid}", func(x *Ctx) (any, error) { return c.Project(ctxOf(x), x.p("pid")) })
	add("PATCH /projects/{pid}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateProjectRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateProject(ctxOf(x), x.A, x.p("pid"), req)
	})
	add("DELETE /projects/{pid}", func(x *Ctx) (any, error) {
		req, err := body[api.ConfirmRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.DeleteProject(ctxOf(x), x.A, x.p("pid"), req.Confirm)
	})
	add("GET /projects/{pid}/members", func(x *Ctx) (any, error) { return c.ProjectGrants(ctxOf(x), x.p("pid")) })
	add("PUT /projects/{pid}/members", func(x *Ctx) (any, error) {
		req, err := body[[]api.ProjectGrant](x)
		if err != nil {
			return nil, err
		}
		return c.SetProjectGrants(ctxOf(x), x.p("pid"), req)
	})
	add("GET /projects/{pid}/usage", func(x *Ctx) (any, error) {
		from, until := x.qint64("from"), x.qint64("until")
		if until == 0 {
			until = c.Now()
		}
		if from == 0 {
			from = until - 30*86400e3
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.UsageResponse, error) {
			return o.Usage(ctx, x.p("pid"), from, until)
		})
	})
	add("POST /projects/{pid}/environments", func(x *Ctx) (any, error) {
		req, err := body[api.CreateEnvironmentRequest](x)
		if err != nil {
			return nil, err
		}
		return c.CreateEnvironment(ctxOf(x), x.A, x.p("pid"), req)
	})
	add("PATCH /environments/{eid}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateEnvironmentRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateEnvironment(ctxOf(x), x.A, x.p("eid"), req)
	})
	add("DELETE /environments/{eid}", func(x *Ctx) (any, error) {
		req, err := body[api.ConfirmRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.DeleteEnvironment(ctxOf(x), x.A, x.p("eid"), req.Confirm)
	})
	add("POST /environments/{eid}/sync", func(x *Ctx) (any, error) {
		req, err := body[api.SyncRequest](x)
		if err != nil {
			return nil, err
		}
		return c.SyncEnvironment(ctxOf(x), x.A, x.p("eid"), req.From)
	})
	add("GET /environments/{eid}/canvas", func(x *Ctx) (any, error) { return c.Canvas(ctxOf(x), x.p("eid")) })
	add("PUT /environments/{eid}/positions", func(x *Ctx) (any, error) {
		req, err := body[api.SavePositionsRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.SavePositions(ctxOf(x), x.A, x.p("eid"), req)
	})
	add("PUT /environments/{eid}/groups", func(x *Ctx) (any, error) {
		req, err := body[[]api.Group](x)
		if err != nil {
			return nil, err
		}
		return c.PutGroups(ctxOf(x), x.A, x.p("eid"), req)
	})
	add("POST /environments/{eid}/presence", func(x *Ctx) (any, error) {
		req, err := body[api.PresenceRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.Presence(ctxOf(x), x.A, x.p("eid"), req)
	})
	add("GET /environments/{eid}/changes", func(x *Ctx) (any, error) { return c.Changes(ctxOf(x), x.p("eid")) })
	add("DELETE /environments/{eid}/changes", func(x *Ctx) (any, error) { return nil, c.DiscardAll(ctxOf(x), x.A, x.p("eid")) })
	add("DELETE /environments/{eid}/changes/{cid}", func(x *Ctx) (any, error) {
		return nil, c.DiscardChange(ctxOf(x), x.A, x.p("eid"), x.p("cid"))
	})
	add("POST /environments/{eid}/changes/commit", func(x *Ctx) (any, error) {
		req, err := body[api.CommitRequest](x)
		if err != nil {
			return nil, err
		}
		r, err := c.Commit(ctxOf(x), x.A, x.p("eid"), req)
		x.Target = r.CommitID
		return r, err
	})
	add("GET /environments/{eid}/activity", func(x *Ctx) (any, error) {
		return c.Activity(ctxOf(x), x.p("eid"), x.q("service"), x.q("kind"), x.q("before"), x.qint("limit", 50))
	})
	add("GET /commits/{id}", func(x *Ctx) (any, error) { return c.CommitDetail(ctxOf(x), x.p("id")) })
	add("GET /events", s.events)
	add("GET /environments/{eid}/shared-variables", func(x *Ctx) (any, error) { return c.Variables(ctxOf(x), x.p("eid"), "") })
	add("PUT /environments/{eid}/shared-variables", func(x *Ctx) (any, error) {
		req, err := body[api.SetVariablesRequest](x)
		if err != nil {
			return nil, err
		}
		return c.SetVariables(ctxOf(x), x.A, x.p("eid"), "", req)
	})
	add("POST /environments/{eid}/shared-variables/reveal", func(x *Ctx) (any, error) {
		req, err := body[api.RevealRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = strings.Join(req.Names, ",")
		return c.Reveal(ctxOf(x), x.p("eid"), "", req)
	})
	add("POST /environments/{eid}/shared-variables/share", func(x *Ctx) (any, error) {
		req, err := body[api.ShareVariableRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.ShareVariable(ctxOf(x), x.A, x.p("eid"), req)
	})
	add("GET /environments/{eid}/export", func(x *Ctx) (any, error) {
		b, err := c.Export(ctxOf(x), x.p("eid"))
		if err != nil {
			return nil, err
		}
		return rawBody{"application/yaml", b}, nil
	})
	add("POST /imports/railway", func(x *Ctx) (any, error) {
		return nil, fail.Unavailable("Import from Railway is not available in this build yet. Use a Docker Compose import or `5050 import compose`, and copy variables with `railway variables --json`.")
	})
	add("POST /environments/{eid}/imports/compose", func(x *Ctx) (any, error) {
		b, err := x.text(1 << 20)
		if err != nil {
			return nil, err
		}
		return c.ImportCompose(ctxOf(x), x.A, x.p("eid"), b)
	})
	add("POST /environments/{eid}/services/{sid}/imports/heroku-env", func(x *Ctx) (any, error) {
		b, err := x.text(1 << 20)
		if err != nil {
			return nil, err
		}
		return c.ImportHeroku(ctxOf(x), x.A, x.p("eid"), x.p("sid"), string(b))
	})
	add("GET /imports/{id}", func(x *Ctx) (any, error) {
		imp, err := c.Import(ctxOf(x), x.p("id"))
		if err != nil {
			return nil, err
		}
		if imp.ProjectID != "" {
			lvl, err := c.ProjectLevel(ctxOf(x), x.A, imp.ProjectID)
			if err != nil || lvl < core.Member {
				return nil, fail.NotFound("The import %s does not exist.", x.p("id"))
			}
		}
		return imp, nil
	})
	add("GET /search", func(x *Ctx) (any, error) { return c.Search(ctxOf(x), x.A, x.q("q")) })

	s.serviceRoutes(add)
	s.templateRoutes(add)
	return m
}

// uploadsDir is where `5050 up` tarballs wait for the orchestrator.
func (s *Server) uploadsDir() string { return filepath.Join(s.C.File().DataDir, "uploads") }

func (s *Server) upload(x *Ctx) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.uploadsDir(), 0o700); err != nil {
		return nil, err
	}
	id := ids.New(ids.Upload)
	p := filepath.Join(s.uploadsDir(), id+".tar.gz")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	const max = 512 << 20
	n, err := copyLimited(f, x.R.Body, max)
	_ = f.Close()
	if err != nil || n == 0 {
		_ = os.Remove(p)
		if n >= max {
			return nil, fail.Validation("body", "The upload is larger than 512 MiB. Add large files to .gitignore or .5050ignore.")
		}
		return nil, fail.Validation("body", "The upload is empty or could not be read.")
	}
	d, err := o.Deploy(x.R.Context(), cpapi.DeployInput{EnvID: x.p("eid"), ServiceID: x.p("sid"), Trigger: "cli", Actor: x.A.Label(),
		Message: x.q("message"), UploadPath: p, Rebuild: true})
	if err != nil {
		_ = os.Remove(p)
		return nil, err
	}
	x.Target = d.ID
	return api.UploadResponse{DeploymentID: d.ID, LogsURL: s.C.ConsoleURL(x.R.Context()) + "/project/" + s.C.EnvProject(x.R.Context(), x.p("eid")) + "/service/" + x.p("sid") + "?environment=" + x.p("eid") + "&deployment=" + d.ID}, nil
}
