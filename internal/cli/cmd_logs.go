// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

// parseTime accepts "5m", "2h", "3d", RFC 3339, a date, or Unix milliseconds,
// and returns Unix milliseconds as a string.
func parseTime(s string, now time.Time) (string, error) {
	if s == "" {
		return "", nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 1e11 {
		return s, nil
	}
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return strconv.FormatInt(now.Add(-time.Duration(n)*24*time.Hour).UnixMilli(), 10), nil
		}
	}
	if strings.HasSuffix(s, "w") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "w")); err == nil {
			return strconv.FormatInt(now.Add(-time.Duration(n)*7*24*time.Hour).UnixMilli(), 10), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return strconv.FormatInt(now.Add(-d).UnixMilli(), 10), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return strconv.FormatInt(t.UnixMilli(), 10), nil
		}
	}
	return "", fmt.Errorf("%q is not a time: use 30m, 2h, 1d, 2026-10-01 or an RFC 3339 time", s)
}

// httpFilter builds the filter expression for the HTTP flags.
func httpFilter(filter, method, status, path, requestID string) string {
	var parts []string
	if filter != "" {
		parts = append(parts, filter)
	}
	if method != "" {
		parts = append(parts, "@method:"+strings.ToUpper(method))
	}
	if status != "" {
		parts = append(parts, "@httpStatus:"+status)
	}
	if path != "" {
		parts = append(parts, "@path:"+quoteIfSpace(path))
	}
	if requestID != "" {
		parts = append(parts, "@requestId:"+requestID)
	}
	return strings.Join(parts, " ")
}

func quoteIfSpace(s string) string {
	if strings.ContainsAny(s, " \t") {
		return strconv.Quote(s)
	}
	return s
}

func (a *App) cmdLogs() *cobra.Command {
	var build, httpLogs, latest, noFollow bool
	var method, status, path, requestID, filter, since, until string
	var lines int
	c := &cobra.Command{
		Use:   "logs [DEPLOYMENT]",
		Short: "Deploy logs (default), build logs, or HTTP logs; follow by default on a TTY",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, len(args) == 0)
			if err != nil {
				return err
			}
			now := time.Now()
			sinceMs, err := parseTime(since, now)
			if err != nil {
				return err
			}
			untilMs, err := parseTime(until, now)
			if err != nil {
				return err
			}
			follow := !noFollow && lines == 0 && until == "" && isTTY(a.Out)
			did := ""
			if len(args) == 1 {
				did = args[0]
			}
			if build {
				if did == "" {
					var ds []api.Deployment
					if err := t.C.Do(ctx, "GET", svcPath(t)+"/deployments?limit=1", nil, &ds); err != nil {
						return err
					}
					if len(ds) == 0 {
						return fmt.Errorf("%s has no deployment yet", t.Service.Config.Name)
					}
					did = ds[0].ID
				}
				return a.buildLogs(ctx, t, did, follow, lines)
			}
			if did == "" && t.Service != nil {
				if latest || t.Service.ActiveDeployment == "" {
					var ds []api.Deployment
					if err := t.C.Do(ctx, "GET", svcPath(t)+"/deployments?limit=1", nil, &ds); err == nil && len(ds) > 0 && !httpLogs {
						did = ds[0].ID
					}
				} else if !httpLogs {
					did = t.Service.ActiveDeployment
				}
			}
			typ := "deploy"
			if httpLogs {
				typ = "http"
			}
			sid := ""
			if t.Service != nil {
				sid = t.Service.ID
			}
			limit := ""
			if lines > 0 {
				limit = strconv.Itoa(lines)
			} else if !follow {
				limit = "500"
			}
			expr := filter
			if httpLogs {
				expr = httpFilter(filter, method, status, path, requestID)
			}
			query := q("environment", t.Env.ID, "services", sid, "deployment", did, "type", typ, "q", expr, "since", sinceMs, "until", untilMs, "limit", limit)
			if follow {
				return t.C.SSE(ctx, "/logs/stream"+query, func(_ string, data []byte) bool {
					a.printLines(data)
					return true
				})
			}
			var lr api.LogsResponse
			if err := t.C.Do(ctx, "GET", "/logs"+query, nil, &lr); err != nil {
				return err
			}
			for _, n := range lr.Notices {
				fmt.Fprintln(a.Err, n)
			}
			for _, l := range lr.Lines {
				a.printLogLine(l, httpLogs)
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVarP(&build, "build", "b", false, "build logs")
	f.BoolVar(&httpLogs, "http", false, "HTTP logs")
	f.StringVar(&method, "method", "", "HTTP method (with --http)")
	f.StringVar(&status, "status", "", "HTTP status, for example 500 or >=400 (with --http)")
	f.StringVar(&path, "path", "", "request path (with --http)")
	f.StringVar(&requestID, "request-id", "", "request id (with --http)")
	f.BoolVar(&latest, "latest", false, "the latest deployment, even if it is not active")
	f.IntVarP(&lines, "lines", "n", 0, "number of lines (stops following)")
	f.StringVarP(&filter, "filter", "f", "", "filter expression, for example @level:error")
	f.StringVarP(&since, "since", "S", "", "start time: 30m, 2h, 1d or a date")
	f.StringVarP(&until, "until", "U", "", "end time")
	f.BoolVar(&noFollow, "no-follow", false, "print and exit")
	return c
}

func (a *App) buildLogs(ctx context.Context, t *Target, did string, follow bool, lines int) error {
	if follow {
		return t.C.SSE(ctx, "/deployments/"+did+"/build-logs?follow=1", func(_ string, data []byte) bool {
			a.printLines(data)
			return true
		})
	}
	var lr api.LogsResponse
	if err := t.C.Do(ctx, "GET", "/deployments/"+did+"/build-logs", nil, &lr); err != nil {
		return err
	}
	ls := lr.Lines
	if lines > 0 && len(ls) > lines {
		ls = ls[len(ls)-lines:]
	}
	for _, l := range ls {
		a.printLine(l)
	}
	return nil
}

func (a *App) printLogLine(l api.LogLine, httpLine bool) {
	if httpLine && l.Msg == "" && !a.JSON {
		at := l.Attrs
		fmt.Fprintf(a.Out, "%v %v %v %vms\n", at["method"], at["path"], at["httpStatus"], at["durationMs"])
		return
	}
	a.printLine(l)
}
