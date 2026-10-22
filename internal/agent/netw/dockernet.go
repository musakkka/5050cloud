// SPDX-License-Identifier: AGPL-3.0-only

package netw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Labels on the Docker objects this package owns.
const (
	labelNetwork     = "5050.network"     // "env" | "build"
	labelEnvironment = "5050.environment" // environment id
	labelInfra       = "5050.infra"       // "caddy"

	optBridgeName = "com.docker.network.bridge.name"
	optICC        = "com.docker.network.bridge.enable_icc"
	optMasquerade = "com.docker.network.bridge.enable_ip_masquerade"
	optMTU        = "com.docker.network.driver.mtu"
)

// dockerAPI is the part of the Docker client this package uses; *client.Client
// satisfies it and tests use a fake.
type dockerAPI interface {
	NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error)
	NetworkInspect(ctx context.Context, id string, options network.InspectOptions) (network.Inspect, error)
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
	NetworkRemove(ctx context.Context, id string) error
	ContainerInspect(ctx context.Context, id string) (container.InspectResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, name string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, id string, options container.StartOptions) error
	ContainerRemove(ctx context.Context, id string, options container.RemoveOptions) error
	ImageInspect(ctx context.Context, id string, opts ...client.ImageInspectOption) (image.InspectResponse, error)
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
}

func newDockerClient(host string) (dockerAPI, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker failed: %w", err)
	}
	return c, nil
}

// bridgeName is the Linux interface of an environment bridge: "f5-" and 12
// hex characters of the environment id's hash (15 characters, the kernel limit).
func bridgeName(envID string) string { return "f5-" + shortHash(envID, 12) }

// networkName is the Docker network of an environment.
func networkName(envID, given string) string {
	if given != "" {
		return given
	}
	return "5050-" + envID
}

// wantNet is one Docker bridge this host must have.
type wantNet struct {
	Name    string
	Env     string // "" for the build network
	Subnet  netip.Prefix
	Gateway netip.Addr
	Bridge  string
	MTU     int
}

func (w wantNet) createOptions() network.CreateOptions {
	opts := map[string]string{
		optBridgeName: w.Bridge,
		optICC:        "true",
		optMasquerade: "true",
	}
	if w.MTU > 0 {
		opts[optMTU] = strconv.Itoa(w.MTU)
	}
	labels := map[string]string{labelNetwork: "env", labelEnvironment: w.Env}
	if w.Env == "" {
		labels = map[string]string{labelNetwork: "build"}
	}
	v6 := false
	return network.CreateOptions{
		Driver:     "bridge",
		EnableIPv6: &v6,
		IPAM:       &network.IPAM{Driver: "default", Config: []network.IPAMConfig{{Subnet: w.Subnet.String(), Gateway: w.Gateway.String()}}},
		Options:    opts,
		Labels:     labels,
	}
}

// matches reports whether an existing network has the settings we want.
func (w wantNet) matches(n network.Inspect) bool {
	if n.Driver != "bridge" || len(n.IPAM.Config) != 1 {
		return false
	}
	c := n.IPAM.Config[0]
	if c.Subnet != w.Subnet.String() || (c.Gateway != "" && c.Gateway != w.Gateway.String()) {
		return false
	}
	if n.Options[optBridgeName] != w.Bridge {
		return false
	}
	mtu := n.Options[optMTU]
	if w.MTU > 0 && mtu != strconv.Itoa(w.MTU) {
		return false
	}
	if w.MTU == 0 && mtu != "" && mtu != "1500" {
		return false
	}
	return true
}

// ensureNetworks creates, fixes and removes the bridges labelled as ours.
// A network that has containers attached is never removed; a mismatch is
// reported and retried on the next apply.
func ensureNetworks(ctx context.Context, d dockerAPI, want []wantNet, removeStale bool) error {
	list, err := d.NetworkList(ctx, network.ListOptions{Filters: filters.NewArgs(filters.Arg("label", labelNetwork))})
	if err != nil {
		return fmt.Errorf("listing Docker networks failed: %w", err)
	}
	have := map[string]network.Summary{}
	for _, n := range list {
		have[n.Name] = n
	}
	var errs []error
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w.Name] = true
		if cur, ok := have[w.Name]; ok {
			insp, err := d.NetworkInspect(ctx, cur.ID, network.InspectOptions{})
			if err != nil {
				errs = append(errs, fmt.Errorf("inspecting Docker network %s failed: %w", w.Name, err))
				continue
			}
			if w.matches(insp) {
				continue
			}
			if len(insp.Containers) > 0 {
				errs = append(errs, fmt.Errorf("Docker network %s does not match the desired state (subnet %s, MTU %d) but still has %d containers attached, so it was left as is until they are gone", w.Name, w.Subnet, w.MTU, len(insp.Containers))) //nolint:staticcheck // sentence starts with a product name
				continue
			}
			if err := d.NetworkRemove(ctx, cur.ID); err != nil {
				errs = append(errs, fmt.Errorf("removing the outdated Docker network %s failed: %w", w.Name, err))
				continue
			}
		}
		if _, err := d.NetworkCreate(ctx, w.Name, w.createOptions()); err != nil {
			errs = append(errs, fmt.Errorf("creating Docker network %s with subnet %s failed: %w", w.Name, w.Subnet, err))
		}
	}
	if removeStale {
		names := make([]string, 0, len(have))
		for n := range have {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			n := have[name]
			if wanted[name] || n.Labels[labelNetwork] != "env" {
				continue
			}
			insp, err := d.NetworkInspect(ctx, n.ID, network.InspectOptions{})
			if err != nil || len(insp.Containers) > 0 {
				continue // still in use; the runtime removes its containers first
			}
			if err := d.NetworkRemove(ctx, n.ID); err != nil && !errdefsNotFound(err) {
				errs = append(errs, fmt.Errorf("removing the unused Docker network %s failed: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}

func errdefsNotFound(err error) bool { return cerrdefs.IsNotFound(err) }
