// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/musakkka/5050cloud/pkg/api"
)

func (a *App) findDomain(ctx context.Context, t *Target, name string) (api.Domain, error) {
	var ds []api.Domain
	if err := t.C.Do(ctx, "GET", svcPath(t)+"/domains", nil, &ds); err != nil {
		return api.Domain{}, err
	}
	for _, d := range ds {
		if d.Domain == name || d.ID == name {
			return d, nil
		}
	}
	return api.Domain{}, fmt.Errorf("%s has no domain %s", t.Service.Config.Name, name)
}

func (a *App) printDomain(d api.Domain) error {
	if a.JSON {
		return a.printJSON(d)
	}
	a.printf("https://%s -> port %d (%s)\n", d.Domain, d.Port, d.Status)
	if d.Message != "" {
		a.printf("%s\n", d.Message)
	}
	if d.Kind == "custom" && !d.DNSOK && len(d.DNS) > 0 {
		a.printf("Add one of these DNS records:\n")
		rows := [][]string{{"TYPE", "NAME", "VALUE"}}
		for _, r := range d.DNS {
			rows = append(rows, []string{r.Type, r.Name, r.Value})
		}
		a.table(a.Out, rows)
	}
	return nil
}

func (a *App) cmdDomain() *cobra.Command {
	var port int
	c := &cobra.Command{
		Use:   "domain [DOMAIN]",
		Short: "No argument: generate a domain; with one: add a custom domain and print the DNS records",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			req := api.CreateDomainRequest{Kind: "generated", Port: port}
			if len(args) == 1 {
				req = api.CreateDomainRequest{Kind: "custom", Domain: args[0], Port: port}
			}
			var d api.Domain
			if err := t.C.Do(ctx, "POST", svcPath(t)+"/domains", req, &d); err != nil {
				return err
			}
			return a.printDomain(d)
		},
	}
	c.Flags().IntVar(&port, "port", 0, "target port (default: the service's port)")
	withDomain := func(use, short string, fn func(ctx context.Context, t *Target, d api.Domain) error) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				t, err := a.resolve(ctx, true)
				if err != nil {
					return err
				}
				d, err := a.findDomain(ctx, t, args[0])
				if err != nil {
					return err
				}
				return fn(ctx, t, d)
			},
		}
	}
	list := &cobra.Command{
		Use: "list", Short: "List the service's domains", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var ds []api.Domain
			if err := t.C.Do(ctx, "GET", svcPath(t)+"/domains", nil, &ds); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(ds)
			}
			rows := [][]string{{"DOMAIN", "KIND", "PORT", "STATUS"}}
			for _, d := range ds {
				rows = append(rows, []string{d.Domain, d.Kind, strconv.Itoa(d.Port), d.Status})
			}
			a.table(a.Out, rows)
			return nil
		},
	}
	del := withDomain("delete DOMAIN", "Delete a domain", func(ctx context.Context, t *Target, d api.Domain) error {
		if err := a.confirm("Delete " + d.Domain + "?"); err != nil {
			return err
		}
		if err := t.C.Do(ctx, "DELETE", "/domains/"+d.ID, nil, nil); err != nil {
			return err
		}
		a.printf("Deleted %s\n", d.Domain)
		return nil
	})
	status := withDomain("status DOMAIN", "Check DNS and the certificate of a domain", func(ctx context.Context, t *Target, d api.Domain) error {
		var out api.Domain
		if err := t.C.Do(ctx, "POST", "/domains/"+d.ID+"/check", nil, &out); err != nil {
			return err
		}
		return a.printDomain(out)
	})
	var newPort int
	update := withDomain("update DOMAIN", "Change the target port of a domain", func(ctx context.Context, t *Target, d api.Domain) error {
		if newPort == 0 {
			return fmt.Errorf("pass --port")
		}
		var out api.Domain
		if err := t.C.Do(ctx, "PATCH", "/domains/"+d.ID, api.UpdateDomainRequest{Port: newPort}, &out); err != nil {
			return err
		}
		return a.printDomain(out)
	})
	update.Flags().IntVar(&newPort, "port", 0, "target port")
	cert := &cobra.Command{Use: "certificate", Short: "Certificate actions"}
	cert.AddCommand(withDomain("retry DOMAIN", "Retry issuing the certificate", func(ctx context.Context, t *Target, d api.Domain) error {
		var out api.Domain
		if err := t.C.Do(ctx, "POST", "/domains/"+d.ID+"/retry-certificate", nil, &out); err != nil {
			return err
		}
		return a.printDomain(out)
	}))
	c.AddCommand(list, del, status, update, cert)
	return c
}

func (a *App) cmdTCPProxy() *cobra.Command {
	c := &cobra.Command{Use: "tcp-proxy", Short: "List, add and delete TCP proxies"}
	list := func(cmd *cobra.Command, args []string) error {
		t, err := a.resolve(cmd.Context(), true)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.printJSON(t.Service.TCPProxies)
		}
		rows := [][]string{{"ID", "ADDRESS", "TARGET PORT"}}
		for _, p := range t.Service.TCPProxies {
			rows = append(rows, []string{p.ID, p.Address, strconv.Itoa(p.TargetPort)})
		}
		a.table(a.Out, rows)
		return nil
	}
	c.RunE = list
	var port int
	add := &cobra.Command{
		Use: "add --port PORT", Short: "Expose a port of the service over TCP", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == 0 {
				return fmt.Errorf("pass --port")
			}
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			var p api.TCPProxy
			if err := t.C.Do(ctx, "POST", svcPath(t)+"/tcp-proxies", api.CreateTCPProxyRequest{TargetPort: port}, &p); err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(p)
			}
			a.printf("%s -> port %d\n", p.Address, p.TargetPort)
			return nil
		},
	}
	add.Flags().IntVar(&port, "port", 0, "target port inside the service")
	del := &cobra.Command{
		Use: "delete ID|PORT", Short: "Delete a TCP proxy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, err := a.resolve(ctx, true)
			if err != nil {
				return err
			}
			for _, p := range t.Service.TCPProxies {
				if p.ID == args[0] || strconv.Itoa(p.TargetPort) == args[0] || strconv.Itoa(p.HostPort) == args[0] {
					if err := a.confirm("Delete the TCP proxy " + p.Address + "?"); err != nil {
						return err
					}
					if err := t.C.Do(ctx, "DELETE", svcPath(t)+"/tcp-proxies/"+p.ID, nil, nil); err != nil {
						return err
					}
					a.printf("Deleted %s\n", p.Address)
					return nil
				}
			}
			return fmt.Errorf("no TCP proxy %s on %s", args[0], t.Service.Config.Name)
		},
	}
	c.AddCommand(&cobra.Command{Use: "list", Short: "List TCP proxies", Args: cobra.NoArgs, RunE: list}, add, del)
	return c
}
