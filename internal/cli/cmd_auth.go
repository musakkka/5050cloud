// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

func (a *App) cmdLogin() *cobra.Command {
	var token string
	var browserless bool
	c := &cobra.Command{
		Use:   "login [URL]",
		Short: "Browser login (device code), stores a token; --token for headless",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			u := ""
			if len(args) == 1 {
				u = normURL(args[0])
			}
			if u == "" {
				u = normURL(a.env("FIFTY_URL"))
			}
			if u == "" {
				u = cfg.Default
			}
			if u == "" {
				if !a.interactive() {
					return errors.New("pass the URL of your install, for example `5050 login https://console.example.com`")
				}
				s, err := a.prompt("URL of your 5050cloud console", "")
				if err != nil {
					return err
				}
				u = normURL(s)
			}
			if u == "" {
				return errors.New("no URL given")
			}
			ctx := cmd.Context()
			cl := newClient(u, "")
			if token == "" {
				token = a.env("FIFTY_TOKEN")
			}
			if token == "" {
				if token, err = a.deviceLogin(ctx, cl, browserless); err != nil {
					return err
				}
			}
			cl.Token = token
			var me api.Me
			if err := cl.Do(ctx, "GET", "/me", nil, &me); err != nil {
				if IsCode(err, api.CodeUnauthorized) {
					return errors.New("that token was refused. Create a new one in Settings > Tokens")
				}
				return err
			}
			cfg.Instances[u] = token
			cfg.Default = u
			if err := cfg.Save(); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(me)
			}
			a.printf("Logged in to %s as %s\n", u, me.User.Email)
			return nil
		},
	}
	c.Flags().StringVar(&token, "token", "", "API token (for headless machines)")
	c.Flags().BoolVar(&browserless, "browserless", false, "print the link instead of opening a browser")
	return c
}

func (a *App) deviceLogin(ctx context.Context, cl *Client, browserless bool) (string, error) {
	var st api.CLIAuthStart
	if err := cl.Do(ctx, "POST", "/cli/auth/start", nil, &st); err != nil {
		return "", err
	}
	fmt.Fprintf(a.Err, "Your code is %s\n", st.Code)
	if browserless || openBrowser(st.VerifyURL) != nil {
		fmt.Fprintf(a.Err, "Open this link to approve the login:\n  %s\n", st.VerifyURL)
	} else {
		fmt.Fprintf(a.Err, "Opened %s in your browser. Approve the login there.\n", st.VerifyURL)
	}
	interval := time.Duration(st.Interval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		var pr api.CLIAuthPollResponse
		if err := cl.Do(ctx, "POST", "/cli/auth/poll", api.CLIAuthPoll{PollToken: st.PollToken}, &pr); err != nil {
			if IsCode(err, api.CodeRateLimited) {
				interval *= 2
				continue
			}
			return "", err
		}
		switch pr.Status {
		case "approved":
			return pr.Token, nil
		case "denied":
			return "", errors.New("the login was denied in the browser")
		case "expired":
			return "", errors.New("the code expired. Run `5050 login` again")
		}
	}
}

func (a *App) cmdLogout() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of the current install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.client()
			if err != nil {
				return err
			}
			_ = c.Do(cmd.Context(), "POST", "/auth/logout", nil, nil)
			cfg, _ := a.config()
			delete(cfg.Instances, c.Base)
			if cfg.Default == c.Base {
				cfg.Default = ""
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			a.printf("Logged out of %s\n", c.Base)
			return nil
		},
	}
}

func (a *App) cmdWhoami() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the logged-in user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.client()
			if err != nil {
				return err
			}
			var me api.Me
			if err := c.Do(cmd.Context(), "GET", "/me", nil, &me); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(me)
			}
			a.printf("%s (%s) on %s\n", me.User.Email, me.User.Role, c.Base)
			return nil
		},
	}
}
