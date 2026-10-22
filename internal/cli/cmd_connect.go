// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

// tunnel listens on 127.0.0.1:port and forwards each connection over the
// tunnel WebSocket to remotePort inside the service's private network.
type tunnel struct {
	ln net.Listener
	wg sync.WaitGroup
}

func (a *App) startTunnel(ctx context.Context, t *Target, localPort, remotePort int) (*tunnel, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)))
	if err != nil {
		return nil, fmt.Errorf("could not listen on port %d: %w", localPort, err)
	}
	tn := &tunnel{ln: ln}
	path := svcPath(t) + "/tunnel?port=" + strconv.Itoa(remotePort)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			tn.wg.Add(1)
			go func() {
				defer tn.wg.Done()
				defer c.Close()
				ws, err := t.C.Dial(ctx, path)
				if err != nil {
					fmt.Fprintln(a.Err, "Error: "+sentence(err.Error()))
					return
				}
				nc := websocket.NetConn(ctx, ws, websocket.MessageBinary)
				defer nc.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(nc, c); done <- struct{}{} }()
				go func() { _, _ = io.Copy(c, nc); done <- struct{}{} }()
				<-done
			}()
		}
	}()
	return tn, nil
}

func (tn *tunnel) port() int { return tn.ln.Addr().(*net.TCPAddr).Port }

func (tn *tunnel) close() { tn.ln.Close() }

// defaultPort is the port of each client's protocol when the URL has none.
var defaultPort = map[string]int{"psql": 5432, "mysql": 3306, "redis-cli": 6379, "mongosh": 27017}

// rewriteURL points a private URL at the local tunnel and returns the remote port.
func rewriteURL(raw, client string, local int) (string, int, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0, errors.New("the connection URL of this service is not valid")
	}
	rp := defaultPort[client]
	if p := u.Port(); p != "" {
		rp, _ = strconv.Atoi(p)
	}
	u.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(local))
	return u.String(), rp, nil
}

// clientCommand builds the argv and extra environment for a database client.
// Passwords go through the environment where the client supports it.
func clientCommand(client, rawURL string, extra []string) (string, []string, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, nil, err
	}
	pass, _ := u.User.Password()
	user := u.User.Username()
	switch client {
	case "psql":
		env := []string{}
		if pass != "" {
			env = append(env, "PGPASSWORD="+pass)
			u.User = url.User(user)
		}
		return "psql", append([]string{u.String()}, extra...), env, nil
	case "mysql":
		args := []string{"-h", u.Hostname(), "-P", u.Port()}
		if user != "" {
			args = append(args, "-u", user)
		}
		args = append(args, extra...)
		if db := strings.TrimPrefix(u.Path, "/"); db != "" {
			args = append(args, db)
		}
		env := []string{}
		if pass != "" {
			env = append(env, "MYSQL_PWD="+pass)
		}
		return "mysql", args, env, nil
	case "redis-cli":
		env := []string{}
		if pass != "" {
			env = append(env, "REDISCLI_AUTH="+pass)
			if user != "" && user != "default" {
				u.User = url.User(user)
			} else {
				u.User = nil
			}
		}
		return "redis-cli", append([]string{"-u", u.String()}, extra...), env, nil
	case "mongosh":
		return "mongosh", append([]string{u.String()}, extra...), nil, nil
	}
	return "", nil, nil, fmt.Errorf("this service has no supported database client (%q)", client)
}

func (a *App) cmdConnect() *cobra.Command {
	var port int
	var tunnelOnly bool
	c := &cobra.Command{
		Use:   "connect [SERVICE] [-- ARGS]",
		Short: "Open psql, mysql, redis-cli or mongosh through a local tunnel; --tunnel-only prints the local URL; -- ARGS passes arguments and stdin through",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dash := cmd.ArgsLenAtDash()
			pos, extra := args, []string(nil)
			if dash >= 0 {
				pos, extra = args[:dash], args[dash:]
			}
			if len(pos) > 1 {
				return errors.New("pass at most one service name")
			}
			if len(pos) == 1 {
				a.Service = pos[0]
			}
			t, err := a.resolve(ctx, false)
			if err != nil {
				return err
			}
			if t.Service == nil {
				if err := a.loadServices(ctx, t); err != nil {
					return err
				}
				var names []string
				var dbs []api.Service
				for _, s := range t.Services {
					if s.Kind == api.KindDatabase {
						names = append(names, s.Config.Name)
						dbs = append(dbs, s)
					}
				}
				if len(dbs) == 1 {
					t.Service = &dbs[0]
				} else {
					i, err := a.pick("Select a database", names)
					if err != nil {
						return err
					}
					t.Service = &dbs[i]
				}
			}
			var ci api.ConnectInfo
			if err := t.C.Do(ctx, "GET", svcPath(t)+"/connect-info", nil, &ci); err != nil {
				return err
			}
			var rr api.RevealResponse
			if err := t.C.Do(ctx, "POST", svcPath(t)+"/variables/reveal", api.RevealRequest{Names: []string{ci.PrivateVariable}, Resolved: true}, &rr); err != nil {
				return err
			}
			raw := rr.Values[ci.PrivateVariable]
			if raw == "" {
				return fmt.Errorf("%s has no value for %s", t.Service.Config.Name, ci.PrivateVariable)
			}
			_, remote, err := rewriteURL(raw, ci.Client, 0)
			if err != nil {
				return err
			}
			tn, err := a.startTunnel(ctx, t, port, remote)
			if err != nil {
				return err
			}
			defer tn.close()
			local, _, _ := rewriteURL(raw, ci.Client, tn.port())
			if tunnelOnly {
				a.printf("%s\n", local)
				fmt.Fprintln(a.Err, "Tunnel open. Press Ctrl+C to close it.")
				<-ctx.Done()
				return nil
			}
			name, argv, env, err := clientCommand(ci.Client, local, extra)
			if err != nil {
				return err
			}
			if _, err := exec.LookPath(name); err != nil {
				return fmt.Errorf("%s is not installed. Install it, or use --tunnel-only and your own client", name)
			}
			return a.runLocal(name, argv, append(os.Environ(), env...))
		},
	}
	c.Flags().IntVarP(&port, "port", "P", 0, "local port (default: a free one)")
	c.Flags().BoolVar(&tunnelOnly, "tunnel-only", false, "print the local URL and keep the tunnel open")
	return c
}
