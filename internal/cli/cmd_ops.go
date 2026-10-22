// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
	"github.com/musakkka/5050cloud/pkg/version"
)

func (a *App) cmdVolume() *cobra.Command {
	c := &cobra.Command{Use: "volume", Short: "List, add, delete and wipe volumes"}
	list := func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		a.Service = ""
		t, err := a.resolve(ctx, false)
		if err != nil {
			return err
		}
		if err := a.loadServices(ctx, t); err != nil {
			return err
		}
		var vs []api.Volume
		rows := [][]string{{"SERVICE", "NAME", "MOUNT", "HOST", "USED"}}
		for _, s := range t.Services {
			if s.Volume != nil {
				vs = append(vs, *s.Volume)
				rows = append(rows, []string{s.Config.Name, s.Volume.Name, s.Volume.MountPath, s.Volume.HostName, humanBytes(s.Volume.UsedBytes)})
			}
		}
		if a.JSON {
			return a.printJSON(vs)
		}
		a.table(a.Out, rows)
		return nil
	}
	c.RunE = list
	var mount, name string
	add := &cobra.Command{
		Use: "add --mount PATH", Short: "Add a volume to the service", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if mount == "" {
				return errors.New("pass --mount PATH")
			}
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var v api.Volume
			if err := t.C.Do(ctx, "POST", svcPath(t)+"/volume", api.CreateVolumeRequest{Name: name, MountPath: mount}, &v); err != nil {
				return err
			}
			if _, err := a.commit(ctx, t, "Add a volume to "+t.Service.Config.Name, true); err != nil {
				return err
			}
			a.printf("Added volume %s at %s on %s\n", v.Name, v.MountPath, t.Service.Config.Name)
			return nil
		},
	}
	add.Flags().StringVar(&mount, "mount", "", "mount path, for example /data")
	add.Flags().StringVar(&name, "name", "", "volume name")
	destructive := func(use, short string, wipe bool) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				t, err := a.resolve(ctx, true)
				if err != nil {
					return err
				}
				v := t.Service.Volume
				if v == nil {
					return fmt.Errorf("%s has no volume", t.Service.Config.Name)
				}
				verb := "Delete"
				if wipe {
					verb = "Wipe every file in"
				}
				if err := a.confirm(fmt.Sprintf("%s the volume %s of %s?", verb, v.Name, t.Service.Config.Name)); err != nil {
					return err
				}
				if wipe {
					if err := t.C.Do(ctx, "POST", svcPath(t)+"/volume/wipe", api.ConfirmRequest{Confirm: v.Name}, nil); err != nil {
						return err
					}
					a.printf("Wiped %s\n", v.Name)
					return nil
				}
				if err := t.C.Do(ctx, "DELETE", svcPath(t)+"/volume", api.ConfirmRequest{Confirm: v.Name}, nil); err != nil {
					return err
				}
				if _, err := a.commit(ctx, t, "Delete the volume of "+t.Service.Config.Name, true); err != nil {
					return err
				}
				a.printf("Deleted %s\n", v.Name)
				return nil
			},
		}
	}
	c.AddCommand(&cobra.Command{Use: "list", Short: "List volumes", Args: cobra.NoArgs, RunE: list}, add,
		destructive("delete", "Delete the service's volume", false), destructive("wipe", "Delete every file in the service's volume", true))
	return c
}

func fmtTime(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}

func (a *App) cmdCron() *cobra.Command {
	c := &cobra.Command{Use: "cron", Short: "List cron runs, or run now"}
	list := func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		t, err := a.resolve(ctx, true)
		if err != nil {
			return err
		}
		var rs []api.CronRun
		if err := t.C.Do(ctx, "GET", svcPath(t)+"/cron-runs", nil, &rs); err != nil {
			return err
		}
		if a.JSON {
			return a.printJSON(rs)
		}
		rows := [][]string{{"SCHEDULED", "STATUS", "TRIGGER", "EXIT", "MESSAGE"}}
		for _, r := range rs {
			exit := "-"
			if r.ExitCode != nil {
				exit = strconv.Itoa(*r.ExitCode)
			}
			rows = append(rows, []string{fmtTime(r.ScheduledAt), r.Status, r.Trigger, exit, r.Message})
		}
		a.table(a.Out, rows)
		return nil
	}
	c.RunE = list
	run := &cobra.Command{
		Use: "run", Short: "Run the cron job now", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var r api.CronRun
			if err := t.C.Do(ctx, "POST", svcPath(t)+"/cron-runs", nil, &r); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(r)
			}
			a.printf("Started run %s of %s\n", r.ID, t.Service.Config.Name)
			return nil
		},
	}
	c.AddCommand(&cobra.Command{Use: "list", Short: "List cron runs", Args: cobra.NoArgs, RunE: list}, run)
	return c
}

// seriesSummary returns the latest, average and maximum of a series.
func seriesSummary(pts [][2]float64) (last, avg, peak float64) {
	if len(pts) == 0 {
		return 0, 0, 0
	}
	peak = math.Inf(-1)
	for _, p := range pts {
		avg += p[1]
		peak = math.Max(peak, p[1])
	}
	return pts[len(pts)-1][1], avg / float64(len(pts)), peak
}

func fmtMetric(name string, v float64) string {
	switch {
	case name == "cpu":
		return fmt.Sprintf("%.2f vCPU", v)
	case name == "mem" || name == "disk":
		return humanBytes(int64(v))
	case name == "rx" || name == "tx":
		return humanBytes(int64(v)) + "/s"
	case name == "error_rate":
		return fmt.Sprintf("%.2f%%", v*100)
	case strings.HasPrefix(name, "p"):
		return fmt.Sprintf("%.0f ms", v)
	case strings.HasPrefix(name, "req_"):
		return fmt.Sprintf("%.2f/s", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func (a *App) cmdMetrics() *cobra.Command {
	var rng string
	c := &cobra.Command{
		Use:   "metrics",
		Short: "Print CPU, memory, network and HTTP summaries for a range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var m api.MetricsResponse
			if err := t.C.Do(ctx, "GET", svcPath(t)+"/metrics"+q("range", rng), nil, &m); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(m)
			}
			order := []string{"cpu", "mem", "disk", "rx", "tx", "req_2xx", "req_3xx", "req_4xx", "req_5xx", "error_rate", "p50", "p90", "p95", "p99"}
			seen := map[string]bool{}
			var names []string
			for _, n := range order {
				if _, ok := m.Series[n]; ok {
					names = append(names, n)
					seen[n] = true
				}
			}
			var rest []string
			for n := range m.Series {
				if !seen[n] {
					rest = append(rest, n)
				}
			}
			sort.Strings(rest)
			names = append(names, rest...)
			a.printf("%s, last %s\n", t.Service.Config.Name, rng)
			rows := [][]string{{"METRIC", "NOW", "AVERAGE", "PEAK"}}
			for _, n := range names {
				last, avg, peak := seriesSummary(m.Series[n])
				rows = append(rows, []string{n, fmtMetric(n, last), fmtMetric(n, avg), fmtMetric(n, peak)})
			}
			a.table(a.Out, rows)
			return nil
		},
	}
	c.Flags().StringVar(&rng, "range", "1h", "range: 1h, 6h, 1d, 7d, 30d")
	return c
}

func dateMs(s string, endOfDay bool) (string, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return parseTime(s, time.Now())
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Millisecond)
	}
	return strconv.FormatInt(t.UnixMilli(), 10), nil
}

func (a *App) cmdUsage() *cobra.Command {
	var from, until string
	c := &cobra.Command{
		Use:   "usage",
		Short: "Usage for the period and the Railway comparison",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			f, err := dateMs(from, false)
			if err != nil {
				return err
			}
			u, err := dateMs(until, true)
			if err != nil {
				return err
			}
			var r api.UsageResponse
			if err := t.C.Do(ctx, "GET", "/projects/"+t.Project.ID+"/usage"+q("from", f, "until", u), nil, &r); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(r)
			}
			a.printf("%s, %s to %s\n", t.Project.Name, fmtTime(r.From), fmtTime(r.Until))
			rows := [][]string{{"SERVICE", "VCPU-H", "MEM GB-H", "EGRESS GB", "VOLUME GB-MO"}}
			for _, s := range append(r.Services, r.Total) {
				name := s.Name
				if name == "" {
					name = "Total"
				}
				rows = append(rows, []string{name, fmt.Sprintf("%.2f", s.VCPUHours), fmt.Sprintf("%.2f", s.MemGBHours), fmt.Sprintf("%.2f", s.EgressGB), fmt.Sprintf("%.2f", s.VolumeGBMonths)})
			}
			a.table(a.Out, rows)
			a.printf("The same usage on Railway: about $%.2f (prices of %s, %s)\n", r.Railway.USD, r.Railway.PricesDate, r.Railway.SourceURL)
			return nil
		},
	}
	c.Flags().StringVar(&from, "from", "", "start date, YYYY-MM-DD (default: start of the period)")
	c.Flags().StringVar(&until, "until", "", "end date, YYYY-MM-DD (default: now)")
	return c
}

func (a *App) cmdConfig() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Config as code"}
	var file string
	pull := &cobra.Command{
		Use: "pull", Short: "Write 5050.yaml for the environment (no lock-in export)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			var raw []byte
			if err := t.C.Do(ctx, "GET", "/environments/"+t.Env.ID+"/export", nil, &raw); err != nil {
				return err
			}
			if file == "-" {
				_, err := a.Out.Write(raw)
				return err
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(a.cwd(), file)
			}
			if err := os.WriteFile(file, raw, 0o644); err != nil {
				return err
			}
			a.printf("Wrote %s\n", file)
			return nil
		},
	}
	pull.Flags().StringVar(&file, "file", "5050.yaml", "output file (- for standard output)")
	c.AddCommand(pull)
	return c
}

func (a *App) findHost(cmd *cobra.Command, c *Client, ref string) (api.Host, error) {
	var hs []api.Host
	if err := c.Do(cmd.Context(), "GET", "/hosts", nil, &hs); err != nil {
		return api.Host{}, err
	}
	for _, h := range hs {
		if h.ID == ref || strings.EqualFold(h.Name, ref) {
			return h, nil
		}
	}
	return api.Host{}, fmt.Errorf("no host named %q. Run `5050 host list`", ref)
}

func (a *App) cmdHost() *cobra.Command {
	c := &cobra.Command{Use: "host", Short: "Add, list, drain and maintain hosts (admin)"}
	var roles []string
	var name string
	add := &cobra.Command{
		Use: "add", Short: "Print the command that joins a new host", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var jt api.JoinToken
			if err := cl.Do(cmd.Context(), "POST", "/hosts/join-tokens", api.CreateJoinTokenRequest{Roles: roles, Name: name}, &jt); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(jt)
			}
			a.printf("Run this on the new host (the token works once, until %s):\n\n  %s\n", fmtTime(jt.ExpiresAt), jt.Command)
			return nil
		},
	}
	add.Flags().StringSliceVar(&roles, "roles", []string{"app"}, "roles: app, build, ingress, data")
	add.Flags().StringVar(&name, "name", "", "host name")
	list := &cobra.Command{
		Use: "list", Short: "List hosts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var hs []api.Host
			if err := cl.Do(cmd.Context(), "GET", "/hosts", nil, &hs); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(hs)
			}
			rows := [][]string{{"NAME", "STATE", "ROLES", "IP", "REPLICAS", "VERSION"}}
			for _, h := range hs {
				rows = append(rows, []string{h.Name, h.State, strings.Join(h.Roles, ","), h.PublicIP, strconv.Itoa(h.Replicas), h.Version})
			}
			a.table(a.Out, rows)
			return nil
		},
	}
	drain := &cobra.Command{
		Use: "drain HOST", Short: "Move replicas off a host and stop scheduling there", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			h, err := a.findHost(cmd, cl, args[0])
			if err != nil {
				return err
			}
			if err := a.confirm("Drain " + h.Name + "?"); err != nil {
				return err
			}
			yes := true
			var out api.Host
			if err := cl.Do(cmd.Context(), "PATCH", "/hosts/"+h.ID, api.UpdateHostRequest{Drain: &yes}, &out); err != nil {
				return err
			}
			a.printf("%s is %s\n", out.Name, out.State)
			return nil
		},
	}
	maint := &cobra.Command{
		Use: "maintenance HOST on|off", Short: "Turn maintenance mode on or off", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != "on" && args[1] != "off" {
				return errors.New("say on or off")
			}
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			h, err := a.findHost(cmd, cl, args[0])
			if err != nil {
				return err
			}
			var out api.Host
			if err := cl.Do(cmd.Context(), "POST", "/hosts/"+h.ID+"/maintenance", api.MaintenanceRequest{On: args[1] == "on"}, &out); err != nil {
				return err
			}
			a.printf("%s is %s\n", out.Name, out.State)
			return nil
		},
	}
	c.AddCommand(add, list, drain, maint)
	return c
}

func (a *App) cmdUpgrade() *cobra.Command {
	var ver string
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the install (control plane then agents), wait and report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, _, _, err := a.client()
			if err != nil {
				return err
			}
			var st api.UpgradeStatus
			if err := cl.Do(ctx, "POST", "/upgrade", api.UpgradeRequest{Version: ver}, &st); err != nil {
				return err
			}
			last := ""
			deadline := time.Now().Add(30 * time.Minute)
			for time.Now().Before(deadline) {
				line := st.State
				if st.Message != "" {
					line += ": " + st.Message
				}
				if line != last && !a.JSON {
					fmt.Fprintf(a.Err, "%s -> %s: %s\n", st.FromVersion, st.ToVersion, line)
					last = line
				}
				switch st.State {
				case "done":
					if a.JSON {
						return a.printJSON(st)
					}
					a.printf("Upgraded to %s\n", st.ToVersion)
					for _, ag := range st.Agents {
						a.printf("  host %s: %s %s\n", ag.HostID, ag.State, ag.Version)
					}
					return nil
				case "rolled_back", "failed":
					if a.JSON {
						_ = a.printJSON(st)
					}
					return &exitError{code: 1, msg: fmt.Sprintf("The upgrade %s. %s", strings.ReplaceAll(st.State, "_", " "), st.Message)}
				case "idle":
					if st.ToVersion == "" || st.ToVersion == st.FromVersion {
						a.printf("Already on the latest version\n")
						return nil
					}
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(3 * time.Second):
				}
				// The control plane restarts during the switch: keep polling.
				var next api.UpgradeStatus
				if err := cl.Do(ctx, "GET", "/upgrade", nil, &next); err == nil {
					st = next
				}
			}
			return errors.New("the upgrade did not finish in 30 minutes. Check Settings > Upgrades")
		},
	}
	c.Flags().StringVar(&ver, "version", "", "version to install (default: latest stable)")
	return c
}

func (a *App) cmdCompletion(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion bash|zsh|fish",
		Short:     "Print a shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(a.Out, true)
			case "zsh":
				return root.GenZshCompletion(a.Out)
			case "fish":
				return root.GenFishCompletion(a.Out, true)
			}
			return fmt.Errorf("unknown shell %q: use bash, zsh or fish", args[0])
		},
	}
}

func (a *App) cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI and server versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := map[string]string{"cli": version.Version, "commit": version.Commit, "date": version.Date}
			if cl, _, _, err := a.client(); err == nil {
				var v api.VersionInfo
				ctx, cancel := ctxTimeout(5 * time.Second)
				defer cancel()
				if cl.Do(ctx, "GET", "/version", nil, &v) == nil {
					out["server"] = v.Version
					out["serverUrl"] = cl.Base
				}
			}
			if a.JSON {
				return a.printJSON(out)
			}
			a.printf("5050 %s (%s, %s)\n", out["cli"], out["commit"], out["date"])
			if out["server"] != "" {
				a.printf("server %s at %s\n", out["server"], out["serverUrl"])
			}
			return nil
		},
	}
}
