// SPDX-License-Identifier: AGPL-3.0-only

package build

import (
	"context"
	"net"
	"sync"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// dockerEngine implements engine with the Docker Engine API. The client is
// created on first use so New never fails.
type dockerEngine struct {
	host string
	once sync.Once
	cli  *client.Client
	err  error
}

func (d *dockerEngine) client() (*client.Client, error) {
	d.once.Do(func() {
		d.cli, d.err = client.NewClientWithOpts(client.WithHost(d.host), client.WithAPIVersionNegotiation())
	})
	return d.cli, d.err
}

func (d *dockerEngine) NetworkGateway(ctx context.Context, name string) (string, error) {
	c, err := d.client()
	if err != nil {
		return "", err
	}
	n, err := c.NetworkInspect(ctx, name, network.InspectOptions{})
	if isNotFound(err) {
		return "", errNoNetwork
	}
	if err != nil {
		return "", err
	}
	for _, cfg := range n.IPAM.Config {
		if ip := net.ParseIP(cfg.Gateway); ip != nil && ip.To4() != nil {
			return cfg.Gateway, nil
		}
	}
	return "", nil
}

func (d *dockerEngine) HostResources(ctx context.Context) (int, int64, error) {
	c, err := d.client()
	if err != nil {
		return 0, 0, err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return 0, 0, err
	}
	return info.NCPU, info.MemTotal, nil
}

func (d *dockerEngine) Container(ctx context.Context, name string) (string, bool, bool, error) {
	c, err := d.client()
	if err != nil {
		return "", false, false, err
	}
	ct, err := c.ContainerInspect(ctx, name)
	if isNotFound(err) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	running := ct.State != nil && ct.State.Running
	img := ""
	if ct.Config != nil {
		img = ct.Config.Image
	}
	return img, running, true, nil
}

// isNotFound matches the Engine API's not-found errors.
func isNotFound(err error) bool {
	return err != nil && client.IsErrNotFound(err)
}
