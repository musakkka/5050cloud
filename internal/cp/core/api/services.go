// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/musakkka/5050cloud/internal/cp/core/auth"
	"github.com/musakkka/5050cloud/internal/cp/core/fail"
	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

const pEnv = "/environments/{eid}"
const pSvc = "/environments/{eid}/services/{sid}"

func copyLimited(w io.Writer, r io.Reader, max int64) (int64, error) {
	n, err := io.Copy(w, io.LimitReader(r, max+1))
	if err == nil && n > max {
		return n, fmt.Errorf("too large")
	}
	return n, err
}

func (s *Server) serviceRoutes(add func(string, handler)) {
	c := s.C
	ctxOf := func(x *Ctx) context.Context { return x.R.Context() }
	eid := func(x *Ctx) string { return x.p("eid") }
	sid := func(x *Ctx) string { return x.p("sid") }

	add("POST "+pEnv+"/services", func(x *Ctx) (any, error) {
		req, err := body[api.CreateServiceRequest](x)
		if err != nil {
			return nil, err
		}
		r, err := c.CreateService(ctxOf(x), x.A, eid(x), req)
		if len(r.Services) > 0 {
			x.Target = r.Services[0].ID
		}
		return r, err
	})
	add("GET "+pSvc, func(x *Ctx) (any, error) { return c.Service(ctxOf(x), eid(x), sid(x)) })
	add("PATCH "+pSvc+"/config", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateServiceConfigRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateServiceConfig(ctxOf(x), x.A, eid(x), sid(x), req.Patch)
	})
	add("PATCH "+pSvc+"/info", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateServiceInfoRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateServiceInfo(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("POST "+pSvc+"/duplicate", func(x *Ctx) (any, error) { return c.DuplicateService(ctxOf(x), x.A, eid(x), sid(x)) })
	add("DELETE "+pSvc, func(x *Ctx) (any, error) {
		req, err := body[api.ConfirmRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.DeleteService(ctxOf(x), x.A, eid(x), sid(x), req.Confirm)
	})
	add("GET "+pSvc+"/variables", func(x *Ctx) (any, error) { return c.Variables(ctxOf(x), eid(x), sid(x)) })
	add("PUT "+pSvc+"/variables", func(x *Ctx) (any, error) {
		req, err := body[api.SetVariablesRequest](x)
		if err != nil {
			return nil, err
		}
		return c.SetVariables(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("POST "+pSvc+"/variables/reveal", func(x *Ctx) (any, error) {
		req, err := body[api.RevealRequest](x)
		if err != nil {
			return nil, err
		}
		x.Target = sid(x)
		x.Detail = strings.Join(req.Names, ",")
		if x.Detail == "" {
			x.Detail = "all"
		}
		return c.Reveal(ctxOf(x), eid(x), sid(x), req)
	})
	add("GET "+pSvc+"/deployments", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) ([]api.Deployment, error) {
			d, err := o.Deployments(ctx, eid(x), sid(x), x.qint("limit", 50), x.q("q"))
			if d == nil && err == nil {
				d = []api.Deployment{}
			}
			return d, err
		})
	})
	add("POST "+pSvc+"/deploy", func(x *Ctx) (any, error) {
		req, err := body[api.DeployRequest](x)
		if err != nil {
			return nil, err
		}
		if _, err := c.ServiceInstance(ctxOf(x), eid(x), sid(x)); err != nil {
			return nil, err
		}
		trigger := "ui"
		if x.A.TokenName != "" {
			trigger = "cli"
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Deployment, error) {
			d, err := o.Deploy(ctx, cpapi.DeployInput{EnvID: eid(x), ServiceID: sid(x), Trigger: trigger, Actor: x.A.Label(), Message: req.Message, Ref: req.Ref, Rebuild: true})
			x.Target = d.ID
			return d, err
		})
	})
	add("POST "+pSvc+"/uploads", s.upload)
	add("GET /deployments/{did}", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.DeploymentDetail, error) {
			return o.Deployment(ctx, x.p("did"))
		})
	})
	for _, act := range []string{"redeploy", "rollback", "restart", "abort", "remove"} {

		add("POST /deployments/{did}/"+act, func(x *Ctx) (any, error) {
			x.Target = x.p("did")
			return orchCall(x, func(o orchestrator, ctx context.Context) (api.Deployment, error) {
				f := map[string]func(context.Context, string, string) (api.Deployment, error){
					"redeploy": o.Redeploy, "rollback": o.Rollback, "restart": o.Restart, "abort": o.Abort, "remove": o.Remove,
				}[act]
				return f(ctx, x.p("did"), x.A.Label())
			})
		})
	}
	add("POST /replicas/{rid}/move", func(x *Ctx) (any, error) {
		req, err := body[api.MoveReplicaRequest](x)
		if err != nil {
			return nil, err
		}
		return orchDo(x, func(o orchestrator, ctx context.Context) error {
			return o.MoveReplica(ctx, x.p("rid"), req.HostID, x.A.Label())
		})
	})
	add("GET /deployments/{did}/build-logs", s.buildLogs)
	add("GET /logs", func(x *Ctx) (any, error) {
		q, err := s.logsQuery(x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.LogsResponse, error) { return o.Logs(ctx, q) })
	})
	add("GET /logs/stream", s.logsStream)
	add("GET "+pSvc+"/metrics", func(x *Ctx) (any, error) {
		from, until := x.timeRange()
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.MetricsResponse, error) {
			return o.Metrics(ctx, eid(x), sid(x), from, until, x.q("view") == "replica")
		})
	})
	add("GET "+pSvc+"/db-metrics", func(x *Ctx) (any, error) {
		from, until := x.timeRange()
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.DBMetricsResponse, error) {
			return o.DBMetrics(ctx, eid(x), sid(x), from, until)
		})
	})
	add("GET "+pSvc+"/monitors", func(x *Ctx) (any, error) { return c.ServiceMonitors(ctxOf(x), eid(x), sid(x)) })
	add("POST "+pSvc+"/monitors", func(x *Ctx) (any, error) {
		req, err := body[api.CreateMonitorRequest](x)
		if err != nil {
			return nil, err
		}
		return c.CreateMonitor(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("PATCH /monitors/{id}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateMonitorRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateMonitor(ctxOf(x), x.p("id"), req)
	})
	add("DELETE /monitors/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteMonitor(ctxOf(x), x.p("id")) })
	add("GET "+pSvc+"/domains", func(x *Ctx) (any, error) { return c.Domains(ctxOf(x), eid(x), sid(x)) })
	add("POST "+pSvc+"/domains", func(x *Ctx) (any, error) {
		req, err := body[api.CreateDomainRequest](x)
		if err != nil {
			return nil, err
		}
		d, err := c.CreateDomain(ctxOf(x), x.A, eid(x), sid(x), req)
		x.Target = d.Domain
		return d, err
	})
	add("PATCH /domains/{id}", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateDomainRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateDomain(ctxOf(x), x.A, x.p("id"), req)
	})
	add("DELETE /domains/{id}", func(x *Ctx) (any, error) { return nil, c.DeleteDomain(ctxOf(x), x.A, x.p("id")) })
	add("POST /domains/{id}/check", func(x *Ctx) (any, error) { return c.CheckDomain(ctxOf(x), x.p("id")) })
	add("POST /domains/{id}/retry-certificate", func(x *Ctx) (any, error) { return c.RetryCertificate(ctxOf(x), x.p("id")) })
	add("POST "+pSvc+"/tcp-proxies", func(x *Ctx) (any, error) {
		req, err := body[api.CreateTCPProxyRequest](x)
		if err != nil {
			return nil, err
		}
		return c.CreateTCPProxy(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("DELETE "+pSvc+"/tcp-proxies/{id}", func(x *Ctx) (any, error) {
		return nil, c.DeleteTCPProxy(ctxOf(x), x.A, eid(x), sid(x), x.p("id"))
	})
	add("POST "+pSvc+"/volume", func(x *Ctx) (any, error) {
		req, err := body[api.CreateVolumeRequest](x)
		if err != nil {
			return nil, err
		}
		return c.CreateVolume(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("GET "+pSvc+"/volume", func(x *Ctx) (any, error) {
		d, err := c.VolumeDetail(ctxOf(x), eid(x), sid(x))
		if err != nil {
			return nil, err
		}
		if o := c.Orchestrator(); o != nil && d.ID != "" {
			if b, err := o.Backups(ctxOf(x), eid(x), sid(x)); err == nil {
				h := b.Health
				d.Backups = &h
			}
		}
		return d, nil
	})
	add("PATCH "+pSvc+"/volume", func(x *Ctx) (any, error) {
		req, err := body[api.UpdateVolumeRequest](x)
		if err != nil {
			return nil, err
		}
		return c.UpdateVolume(ctxOf(x), x.A, eid(x), sid(x), req)
	})
	add("DELETE "+pSvc+"/volume", func(x *Ctx) (any, error) {
		req, err := body[api.ConfirmRequest](x)
		if err != nil {
			return nil, err
		}
		return nil, c.DeleteVolume(ctxOf(x), x.A, eid(x), sid(x), req.Confirm)
	})
	add("POST "+pSvc+"/volume/wipe", func(x *Ctx) (any, error) {
		req, err := body[api.ConfirmRequest](x)
		if err != nil {
			return nil, err
		}
		name, err := c.VolumeName(ctxOf(x), eid(x), sid(x))
		if err != nil {
			return nil, err
		}
		if req.Confirm != name {
			return nil, fail.Validation("confirm", "Type the volume name %q to confirm.", name)
		}
		return orchDo(x, func(o orchestrator, ctx context.Context) error { return o.WipeVolume(ctx, eid(x), sid(x), x.A.Label()) })
	})
	add("GET "+pSvc+"/backups", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.BackupsResponse, error) {
			return o.Backups(ctx, eid(x), sid(x))
		})
	})
	add("POST "+pSvc+"/backups", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Backup, error) {
			return o.BackupNow(ctx, eid(x), sid(x), x.A.Label())
		})
	})
	add("PUT "+pSvc+"/backups/schedule", func(x *Ctx) (any, error) {
		req, err := body[api.BackupSchedule](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.BackupSchedule, error) {
			return o.SetBackupSchedule(ctx, eid(x), sid(x), req)
		})
	})
	add("POST /backups/{id}/lock", func(x *Ctx) (any, error) {
		req, err := body[api.LockBackupRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Backup, error) {
			return o.LockBackup(ctx, x.p("id"), req.Locked)
		})
	})
	add("DELETE /backups/{id}", func(x *Ctx) (any, error) {
		return orchDo(x, func(o orchestrator, ctx context.Context) error { return o.DeleteBackup(ctx, x.p("id")) })
	})
	add("POST "+pSvc+"/restore", func(x *Ctx) (any, error) {
		req, err := body[api.RestoreRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.RestoreResponse, error) {
			return o.Restore(ctx, eid(x), sid(x), req, x.A.Label())
		})
	})
	add("POST "+pSvc+"/promote", func(x *Ctx) (any, error) {
		req, err := body[api.PromoteRequest](x)
		if err != nil {
			return nil, err
		}
		return orchDo(x, func(o orchestrator, ctx context.Context) error {
			return o.Promote(ctx, eid(x), sid(x), req, x.A.Label())
		})
	})
	add("POST "+pSvc+"/major-upgrade", func(x *Ctx) (any, error) {
		req, err := body[api.MajorUpgradeRequest](x)
		if err != nil {
			return nil, err
		}
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.RestoreResponse, error) {
			return o.MajorUpgrade(ctx, eid(x), sid(x), req, x.A.Label())
		})
	})
	add("GET "+pSvc+"/connect-info", func(x *Ctx) (any, error) { return s.connectInfo(x) })
	add("POST "+pSvc+"/regenerate-credentials", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.CommitResponse, error) {
			return o.RegenerateCredentials(ctx, eid(x), sid(x), x.A.Label())
		})
	})
	add("GET "+pSvc+"/data", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.DataResponse, error) {
			return o.DataSummary(ctx, eid(x), sid(x))
		})
	})
	add("GET "+pSvc+"/extensions", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) ([]api.Extension, error) {
			return o.Extensions(ctx, eid(x), sid(x))
		})
	})
	add("POST "+pSvc+"/extensions", func(x *Ctx) (any, error) {
		req, err := body[api.EnableExtensionRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = req.Name
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.Extension, error) {
			return o.EnableExtension(ctx, eid(x), sid(x), req.Name, x.A.Label())
		})
	})
	add("GET "+pSvc+"/redis/keys", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.RedisKeysPage, error) {
			return o.RedisKeys(ctx, eid(x), sid(x), x.q("match"), x.q("cursor"))
		})
	})
	add("POST "+pSvc+"/redis/keys/reveal", func(x *Ctx) (any, error) {
		req, err := body[api.RedisRevealRequest](x)
		if err != nil {
			return nil, err
		}
		x.Detail = req.Key
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.RedisRevealResponse, error) {
			return o.RevealRedisKey(ctx, eid(x), sid(x), req.Key)
		})
	})
	add("GET "+pSvc+"/cron-runs", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) ([]api.CronRun, error) {
			r, err := o.CronRuns(ctx, eid(x), sid(x), x.qint("limit", 50))
			if r == nil && err == nil {
				r = []api.CronRun{}
			}
			return r, err
		})
	})
	add("POST "+pSvc+"/cron-runs", func(x *Ctx) (any, error) {
		return orchCall(x, func(o orchestrator, ctx context.Context) (api.CronRun, error) {
			return o.RunCronNow(ctx, eid(x), sid(x), x.A.Label())
		})
	})
	add("GET "+pSvc+"/shell", s.shell)
	add("GET "+pSvc+"/tunnel", s.tunnel)
}

// connectInfo returns how to connect to a database service from its resolved variables.
func (s *Server) connectInfo(x *Ctx) (any, error) {
	env, _, err := s.C.ResolveVariables(x.R.Context(), x.p("eid"), x.p("sid"), nil)
	if err != nil {
		return nil, err
	}
	svc, err := s.C.Service(x.R.Context(), x.p("eid"), x.p("sid"))
	if err != nil {
		return nil, err
	}
	ci, err := connectInfoFor(svc, env)
	if err != nil {
		return nil, err
	}
	x.Target = x.p("sid")
	return ci, nil
}

// ---------------------------------------------------------------- readyz

func (s *Server) readyz(x *Ctx) (any, error) {
	ctx := x.R.Context()
	checks := api.ReadyChecks{DB: s.C.Store().Ping(ctx) == nil, Migrations: s.C.Store().MigrationsApplied(ctx)}
	if o := s.C.Orchestrator(); o != nil {
		checks.Hub, checks.Agents = o.Ready(ctx)
	}
	ready := checks.DB && checks.Migrations && checks.Hub && !brokenReadyz
	r := api.Readyz{Version: version.Version, Ready: ready, ReadOnly: s.C.ReadOnly()}
	if auth.IsLocal(x.R) || x.A.UserID != "" {
		r.Checks = &checks
	}
	if !ready {
		writeJSON(x.W, http.StatusServiceUnavailable, r)
		return handled{}, nil
	}
	return r, nil
}

// ---------------------------------------------------------------- SSE

func sseStart(w http.ResponseWriter) (http.Flusher, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return f, true
}

func sseSend(w http.ResponseWriter, f http.Flusher, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	f.Flush()
	return nil
}

func (s *Server) events(x *Ctx) (any, error) {
	env := x.q("environment")
	sub := s.C.Broker().Subscribe(env, x.A.UserID, 512)
	defer sub.Close()
	f, ok := sseStart(x.W)
	if !ok {
		return nil, fail.Internal(fmt.Errorf("streaming unsupported"))
	}
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	ctx := x.R.Context()
	for {
		select {
		case <-ctx.Done():
			return handled{}, nil
		case e := <-sub.C:
			if e.Origin != "" && e.Origin == x.A.SessionHash {
				continue
			}
			if err := sseSend(x.W, f, e); err != nil {
				return handled{}, nil
			}
		case <-t.C:
			if n := sub.Dropped(); n > 0 {
				_ = sseSend(x.W, f, api.Event{Type: "resync", EnvironmentID: env})
			}
			if _, err := fmt.Fprint(x.W, ": ping\n\n"); err != nil {
				return handled{}, nil
			}
			f.Flush()
		}
	}
}

func (s *Server) logsQuery(x *Ctx) (cpapi.LogsQuery, error) {
	q := cpapi.LogsQuery{EnvID: x.q("environment"), DeploymentID: x.q("deployment"), Filter: x.q("q"), Since: x.qint64("since"),
		Until: x.qint64("until"), Limit: x.qint("limit", 500), Around: x.q("around")}
	if v := x.q("services"); v != "" {
		q.ServiceIDs = strings.Split(v, ",")
	}
	if v := x.q("type"); v != "" {
		q.Types = strings.Split(v, ",")
	}
	if q.EnvID == "" && q.DeploymentID == "" {
		return q, fail.Validation("environment", "Pass environment= or deployment=.")
	}
	if q.EnvID != "" && q.DeploymentID != "" {
		// A deployment id from another environment must not widen the query.
		o, err := x.orch()
		if err != nil {
			return q, err
		}
		d, err := o.Deployment(x.R.Context(), q.DeploymentID)
		if err != nil || d.EnvironmentID != q.EnvID {
			return q, fail.NotFound("The deployment does not exist.")
		}
	}
	if q.EnvID == "" {
		o, err := x.orch()
		if err != nil {
			return q, err
		}
		d, err := o.Deployment(x.R.Context(), q.DeploymentID)
		if err != nil {
			return q, err
		}
		q.EnvID = d.EnvironmentID
		pid := s.C.EnvProject(x.R.Context(), q.EnvID)
		lvl, err := s.C.ProjectLevel(x.R.Context(), x.A, pid)
		if err != nil || lvl < 1 {
			return q, fail.NotFound("The deployment does not exist.")
		}
	}
	if len(q.ServiceIDs) > 0 {
		// Every requested service must belong to the authorized environment:
		// agents filter by service id only, so a foreign id would return
		// another tenant's lines from a shared host.
		snaps, err := s.C.ServiceInstances(x.R.Context(), q.EnvID)
		if err != nil {
			return q, err
		}
		own := make(map[string]bool, len(snaps))
		for _, sn := range snaps {
			own[sn.ServiceID] = true
		}
		for _, id := range q.ServiceIDs {
			if !own[id] {
				return q, fail.NotFound("The service %s does not exist in this environment.", id)
			}
		}
	}
	if q.Limit < 1 || q.Limit > 5000 {
		q.Limit = 500
	}
	return q, nil
}

func (s *Server) logsStream(x *Ctx) (any, error) {
	q, err := s.logsQuery(x)
	if err != nil {
		return nil, err
	}
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	f, ok := sseStart(x.W)
	if !ok {
		return nil, fail.Internal(fmt.Errorf("streaming unsupported"))
	}
	_ = o.TailLogs(x.R.Context(), q, func(lines []api.LogLine) error { return sseSend(x.W, f, lines) })
	return handled{}, nil
}

func (s *Server) buildLogs(x *Ctx) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	did := x.p("did")
	if x.q("follow") != "1" {
		var out api.LogsResponse
		err := o.BuildLogs(x.R.Context(), did, false, func(l []api.LogLine) error { out.Lines = append(out.Lines, l...); return nil })
		if out.Lines == nil {
			out.Lines = []api.LogLine{}
		}
		return out, err
	}
	f, ok := sseStart(x.W)
	if !ok {
		return nil, fail.Internal(fmt.Errorf("streaming unsupported"))
	}
	_ = o.BuildLogs(x.R.Context(), did, true, func(l []api.LogLine) error { return sseSend(x.W, f, l) })
	return handled{}, nil
}

// ---------------------------------------------------------------- websockets

func (s *Server) accept(x *Ctx) (*websocket.Conn, error) {
	return websocket.Accept(x.W, x.R, &websocket.AcceptOptions{})
}

func (s *Server) shell(x *Ctx) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	cols, _ := strconv.Atoi(x.q("cols"))
	rows, _ := strconv.Atoi(x.q("rows"))
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	s.audit(x)
	conn, err := s.accept(x)
	if err != nil {
		return handled{}, nil
	}
	defer func() { _ = conn.CloseNow() }()
	ctx, cancel := context.WithCancel(x.R.Context())
	defer cancel()
	pr, pw := io.Pipe()
	resize := make(chan [2]int, 4)
	go func() {
		defer func() { _ = pw.Close() }()
		for {
			typ, b, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			if typ == websocket.MessageText {
				var ctl api.ShellControl
				if json.Unmarshal(b, &ctl) == nil && ctl.Resize != nil {
					select {
					case resize <- *ctl.Resize:
					default:
					}
				}
				continue
			}
			if _, err := pw.Write(b); err != nil {
				return
			}
		}
	}()
	rw := &wsRW{r: pr, ctx: ctx, c: conn}
	code, err := o.Shell(ctx, x.p("eid"), x.p("sid"), x.q("replica"), x.q("session"), nil, cols, rows, rw, resize)
	if err != nil {
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
		_ = conn.Close(websocket.StatusInternalError, "shell failed")
		return handled{}, nil
	}
	b, _ := json.Marshal(api.ShellControl{Exit: &code})
	_ = conn.Write(context.Background(), websocket.MessageText, b)
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return handled{}, nil
}

type wsRW struct {
	r   io.Reader
	ctx context.Context
	c   *websocket.Conn
}

func (w *wsRW) Read(p []byte) (int, error) { return w.r.Read(p) }
func (w *wsRW) Write(p []byte) (int, error) {
	if err := w.c.Write(w.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Server) tunnel(x *Ctx) (any, error) {
	o, err := x.orch()
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(x.q("port"))
	if err != nil || port < 1 || port > 65535 {
		return nil, fail.Validation("port", "Pass port= between 1 and 65535.")
	}
	s.audit(x)
	conn, err := s.accept(x)
	if err != nil {
		return handled{}, nil
	}
	nc := websocket.NetConn(x.R.Context(), conn, websocket.MessageBinary)
	defer func() { _ = nc.Close() }()
	if err := o.Tunnel(x.R.Context(), x.p("eid"), x.p("sid"), port, nc); err != nil {
		_ = conn.Close(websocket.StatusInternalError, sentenceCut(err.Error()))
	}
	return handled{}, nil
}

func sentenceCut(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
