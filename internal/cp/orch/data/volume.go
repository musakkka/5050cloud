// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/musakkka/5050cloud/internal/cp/cpapi"
	"github.com/musakkka/5050cloud/pkg/proto"
)

// WipeVolume stops the service, empties its volume and starts it again
// (docs/SPEC.md 9.12). The API layer checks the typed confirmation and writes
// the audit entry.
func (d *Data) WipeVolume(ctx context.Context, envID, serviceID, actor string) error {
	snap, err := d.instance(ctx, envID, serviceID)
	if err != nil {
		return err
	}
	if snap.Volume == nil {
		return errors.New("This service has no volume to wipe.")
	}
	host, err := d.run.VolumeHost(ctx, envID, serviceID)
	if err != nil {
		return fmt.Errorf("The host of the volume cannot be found: %w", err)
	}
	if host == "" {
		return errors.New("The volume has not been created yet, so it is already empty.")
	}
	if err := d.claim(envID, serviceID, "the wipe"); err != nil {
		return err
	}
	defer d.release(envID, serviceID)
	image, err := d.cat.HelperImage()
	if err != nil {
		return err
	}
	files, err := d.sharedFiles("wipe-volume")
	if err != nil {
		return err
	}
	_, _, running := d.run.ActiveReplica(ctx, envID, serviceID)
	if err := d.run.StopService(ctx, envID, serviceID, actor); err != nil {
		return fmt.Errorf("The service could not be stopped before the wipe: %w", err)
	}
	out := d.runJob(ctx, host, proto.Job{
		Kind: "maintenance", Image: image, Cmd: []string{toolDir + "wipe-volume"},
		Mounts:     []proto.Mount{{Kind: "volume", Source: volumeName(snap.Volume.ID), Target: "/data"}},
		Files:      files,
		TimeoutSec: wipeTimeoutSec, Labels: jobLabels(envID, serviceID, "wipe"),
	})
	var wipeErr error
	if !out.ok() {
		wipeErr = errors.New(jobError("Wiping the volume", out))
	}
	// Start the service again whether or not the wipe worked: it was running.
	if running == nil {
		if _, err := d.run.Deploy(ctx, cpapi.DeployInput{EnvID: envID, ServiceID: serviceID, Trigger: "redeploy", Actor: actor,
			Message: "Volume wiped"}); err != nil {
			if wipeErr != nil {
				return fmt.Errorf("%v The service could not be started again either: %w", wipeErr, err)
			}
			return fmt.Errorf("The volume was wiped, but the service could not be started again: %w", err)
		}
	}
	if wipeErr != nil {
		return wipeErr
	}
	d.activity(ctx, envID, serviceID, fmt.Sprintf("Wiped the volume of %s.", snap.Config.Name), actor, "")
	return nil
}
