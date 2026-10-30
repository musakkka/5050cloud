// SPDX-License-Identifier: AGPL-3.0-only

// Command 5050-agent runs on every 5050cloud host. It joins the install,
// keeps a connection to the control plane and makes the host run what the
// control plane wants (docs/SPEC.md 7 and 8.1).
//
//	5050-agent join --url https://console.example.com --token J-...
//	5050-agent run
//	5050-agent rejoin --url https://console.example.com --token J-...
//	5050-agent net-restore
//	5050-agent version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/musakkka/5050cloud/internal/agent/build"
	"github.com/musakkka/5050cloud/internal/agent/core"
	"github.com/musakkka/5050cloud/internal/agent/core/statecache"
	"github.com/musakkka/5050cloud/internal/agent/core/tools"
	"github.com/musakkka/5050cloud/internal/agent/netw"
	"github.com/musakkka/5050cloud/internal/upgrade"
	"github.com/musakkka/5050cloud/pkg/version"
)

const usage = `5050-agent runs on every 5050cloud host.

Usage:
  5050-agent join --url URL --token TOKEN [flags]   join this host to an install
  5050-agent run [flags]                            run the agent (systemd starts it)
  5050-agent rejoin --url URL --token TOKEN [flags] join again with a new identity
  5050-agent net-restore [flags]                    restore firewall and mesh from the cache (boot, before Docker)
  5050-agent version                                print the version

Run "5050-agent <command> -h" for the flags of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// common are the path flags every command shares.
type common struct {
	config, hostKey, dataDir, docker string
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.config, "config", envOr("FIFTY_AGENT_CONFIG", defaultConfig), "agent credentials file")
	fs.StringVar(&c.hostKey, "host-key", envOr("FIFTY_HOST_KEY", defaultHostKey), "host key that encrypts the desired-state cache")
	fs.StringVar(&c.dataDir, "data-dir", envOr("FIFTY_DATA_DIR", defaultDataDir), "data directory")
	fs.StringVar(&c.docker, "docker-host", envOr("DOCKER_HOST", defaultDocker), "Docker Engine address")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "join":
		err = cmdJoin(rest, stdout, stderr, false)
	case "rejoin":
		err = cmdJoin(rest, stdout, stderr, true)
	case "run":
		err = cmdRun(rest, stderr)
	case "net-restore":
		err = cmdNetRestore(rest, stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "5050-agent %s (commit %s, built %s, %s/%s)\n", version.Version, version.Commit, version.Date, runtime.GOOS, runtime.GOARCH)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "Unknown command %q.\n\n%s", cmd, usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(stderr, "Error: "+finish(ue.msg))
			return 2
		}
		fmt.Fprintln(stderr, "Error: "+finish(err.Error()))
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

// finish makes a message read as a sentence.
func finish(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Unknown error."
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func cmdJoin(args []string, stdout, stderr io.Writer, rejoin bool) error {
	name := "join"
	if rejoin {
		name = "rejoin"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c common
	c.register(fs)
	var p joinParams
	fs.StringVar(&p.URL, "url", os.Getenv("FIFTY_JOIN"), "console URL, for example https://console.example.com")
	fs.StringVar(&p.Token, "token", os.Getenv("FIFTY_TOKEN"), "single-use join token from Hosts > Add host")
	fs.StringVar(&p.Replace, "replace", "", "re-adopt this host id (rebuilding a dead control host)")
	fs.StringVar(&p.CAPin, "ca-pin", os.Getenv("FIFTY_CA_PIN"), "pin the console certificate: sha256:<hex>")
	fs.StringVar(&p.CACertFile, "ca-cert", "", "extra CA certificate (PEM) that signs the console certificate")
	fs.StringVar(&p.PublicIP, "public-ip", os.Getenv("FIFTY_PUBLIC_IP"), "public IPv4 address of this host")
	noRestart := fs.Bool("no-restart", false, "rejoin: do not restart 5050-agent.service")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if p.URL == "" {
		return usageError{"the console URL is required (--url https://console.example.com)"}
	}
	if p.Token == "" {
		return usageError{"a join token is required (--token). Create one in the console under Hosts > Add host"}
	}
	if !rejoin {
		if old, err := loadConfig(c.config); err == nil {
			return fmt.Errorf("this host already joined as %s (%s). Use 5050-agent rejoin to join again with a new identity", old.HostID, c.config)
		}
	} else if err := resetIdentity(c, stdout); err != nil {
		return err
	}
	nw := netw.New(netw.Options{DataDir: c.dataDir, DockerHost: c.docker})
	p.ConfigPath, p.DataDir, p.HostKey, p.DockerHost = c.config, c.dataDir, c.hostKey, c.docker
	p.PublicKey = nw.PublicKey
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg, err := doJoin(ctx, p)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Joined as host %s with roles %s.\n", cfg.HostID, strings.Join(cfg.Roles, ", "))
	if cfg.Mesh.Address != "" {
		fmt.Fprintf(stdout, "Mesh address: %s\n", cfg.Mesh.Address)
	}
	fmt.Fprintf(stdout, "Credentials saved to %s (0600).\n", c.config)
	if cfg.RegistryAddress != "" {
		fmt.Fprintf(stdout, "registry-address: %s\n", cfg.RegistryAddress)
	}
	if rejoin && !*noRestart {
		if err := systemctl("restart", "5050-agent.service"); err != nil {
			fmt.Fprintf(stdout, "Restart the agent to use the new identity: sudo systemctl restart 5050-agent (%s).\n", strings.TrimSuffix(err.Error(), "."))
		} else {
			fmt.Fprintln(stdout, "Restarted 5050-agent.service with the new identity.")
		}
	} else if !rejoin {
		fmt.Fprintln(stdout, "Start the agent with: sudo systemctl enable --now 5050-agent")
	}
	return nil
}

// resetIdentity moves the old credentials aside and removes the WireGuard
// key and the desired-state cache, so the host joins as a new machine.
// Containers keep running until the new identity's desired state arrives.
func resetIdentity(c common, stdout io.Writer) error {
	if _, err := os.Stat(c.config); err == nil {
		bak := fmt.Sprintf("%s.old-%d", c.config, time.Now().Unix())
		if err := os.Rename(c.config, bak); err != nil {
			return fmt.Errorf("could not move %s aside: %w", c.config, err)
		}
		fmt.Fprintf(stdout, "Moved the old credentials to %s.\n", bak)
	}
	for _, p := range []string{filepath.Join(c.dataDir, "wg.key"), statecache.Path(c.dataDir)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not remove %s: %w", p, err)
		}
	}
	return nil
}

// systemctl runs systemctl with the given arguments when systemd is present.
func systemctl(args ...string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return errors.New("systemctl was not found")
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func cmdRun(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c common
	c.register(fs)
	debug := fs.Bool("debug", os.Getenv("FIFTY_DEBUG") != "", "log debug messages")
	if err := fs.Parse(args); err != nil {
		return err
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	cfg, err := loadConfig(c.config)
	if err != nil {
		return err
	}
	hostKey, err := statecache.LoadOrCreateKey(c.hostKey)
	if err != nil {
		return fmt.Errorf("could not read the host key %s: %w", c.hostKey, err)
	}
	machineID, err := readMachineID("")
	if err != nil {
		return err
	}
	caPEM, err := readCACert(cfg.CACertFile)
	if err != nil {
		return err
	}
	live := core.NewLive(cfg.HostToken)
	tm := tools.New(filepath.Join(c.dataDir, "tools"), nil)
	nw := netw.New(netw.Options{DataDir: c.dataDir, DockerHost: c.docker, Logger: log.With("part", "network")})
	ing := netw.NewIngress(netw.IngressOptions{DataDir: c.dataDir, DockerHost: c.docker, Logger: log.With("part", "ingress")})
	b := build.New(build.Options{
		DataDir: c.dataDir, DockerHost: c.docker, Tools: tm, Network: nw,
		HostToken: live.Token, Images: live.Images,
	})
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log.Info("starting 5050-agent", "version", version.Version, "host", cfg.HostID, "control", cfg.ControlURL)
	return core.Run(ctx, core.Options{
		ControlURL: cfg.ControlURL, HostToken: cfg.HostToken, DataDir: c.dataDir, DockerHost: c.docker,
		Network: nw, Ingress: ing, Builder: b, CACertPEM: caPEM, HostKey: hostKey,
		HostID: cfg.HostID, MachineID: machineID, CAPin: cfg.CAPin, Tools: tm, Logger: log,
		PublicIP: cfg.PublicIP, Live: live,
		OnTokenRotate: func(token string) error {
			cur, err := loadConfig(c.config)
			if err != nil {
				return err
			}
			cur.HostToken = token
			return saveConfig(c.config, cur)
		},
		Upgrade: upgrade.Options{InstallDir: "/opt/5050", DataDir: c.dataDir, Arch: runtime.GOARCH},
	})
}

// cmdNetRestore is what 5050-net-restore.service runs at boot, before
// Docker starts: it loads the firewall chains and wg5050 from the cached
// desired state, so a quarantined environment never has open egress
// (docs/SPEC.md 7.2). Whatever happens to the cache, the metadata drop goes in
// first, so no container starts without it.
func cmdNetRestore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("net-restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c common
	c.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	baseErr := netw.RestoreBaseline(ctx, netw.Options{DataDir: c.dataDir, DockerHost: c.docker})
	if baseErr != nil {
		fmt.Fprintf(stderr, "Could not load the metadata drop: %v\n", baseErr)
	}
	key, err := os.ReadFile(c.hostKey)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "No host key yet; nothing more to restore.")
		return baseErr
	}
	if err != nil {
		return fmt.Errorf("could not read the host key %s: %w", c.hostKey, err)
	}
	cache, err := statecache.New(statecache.Path(c.dataDir), key)
	if err != nil {
		return err
	}
	e, err := cache.Load()
	if errors.Is(err, statecache.ErrNoCache) {
		fmt.Fprintln(stdout, "No cached desired state yet; nothing more to restore.")
		return baseErr
	}
	if err != nil {
		return fmt.Errorf("could not read the desired-state cache: %w", err)
	}
	if err := netw.Restore(ctx, netw.Options{DataDir: c.dataDir, DockerHost: c.docker}, e.State); err != nil {
		return fmt.Errorf("could not restore the firewall and mesh from the cache (generation %d): %w", e.State.Generation, err)
	}
	fmt.Fprintf(stdout, "Restored the firewall and mesh from the cached desired state (generation %d).\n", e.State.Generation)
	return nil
}
