// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

// stty runs stty against the terminal on stdin (Unix only).
func stty(args ...string) (string, error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("not supported")
	}
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	out, err := c.Output()
	return strings.TrimSpace(string(out)), err
}

func termSize() (cols, rows int) {
	cols, rows = 80, 24
	if s, err := stty("size"); err == nil {
		if f := strings.Fields(s); len(f) == 2 {
			r, _ := strconv.Atoi(f[0])
			c, _ := strconv.Atoi(f[1])
			if r > 0 && c > 0 {
				rows, cols = r, c
			}
		}
	}
	return cols, rows
}

func (a *App) cmdSSH() *cobra.Command {
	var replica, session string
	c := &cobra.Command{
		Use:   "ssh [-- CMD]",
		Short: "Shell inside a running replica (WebSocket exec)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			tty := a.interactive() && len(args) == 0
			cols, rows := 80, 24
			if tty {
				cols, rows = termSize()
			}
			ws, err := t.C.Dial(ctx, svcPath(t)+"/shell"+q("replica", replica, "cols", strconv.Itoa(cols), "rows", strconv.Itoa(rows), "session", session))
			if err != nil {
				return err
			}
			defer ws.CloseNow()
			if tty {
				if saved, err := stty("-g"); err == nil {
					_, _ = stty("raw", "-echo")
					defer func() { _, _ = stty(saved) }()
				}
			}
			if len(args) > 0 {
				line := strings.Join(args, " ") + "\nexit $?\n"
				if err := ws.Write(ctx, websocket.MessageBinary, []byte(line)); err != nil {
					return err
				}
			}
			go func() {
				buf := make([]byte, 32*1024)
				for {
					n, err := a.In.Read(buf)
					if n > 0 {
						if ws.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
			for {
				typ, data, err := ws.Read(ctx)
				if err != nil {
					if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
						return nil
					}
					if ctx.Err() != nil {
						return nil
					}
					return errors.New("the shell connection closed")
				}
				if typ == websocket.MessageBinary {
					_, _ = a.Out.Write(data)
					continue
				}
				var ctl api.ShellControl
				if json.Unmarshal(data, &ctl) == nil && ctl.Exit != nil {
					if *ctl.Exit != 0 {
						return &exitError{code: *ctl.Exit}
					}
					return nil
				}
			}
		},
	}
	c.Flags().StringVar(&replica, "replica", "", "replica id (default: the first running one)")
	c.Flags().StringVar(&session, "session", "", "session name to reattach (kept for 10 minutes)")
	return c
}
