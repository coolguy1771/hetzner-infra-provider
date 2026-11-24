// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements the Hetzner Cloud infra provider core.
package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/apricote/hcloud-upload-image/hcloudimages/v2"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/siderolabs/omni/client/pkg/imagefactory"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.uber.org/zap"

	"github.com/coolguy1771/hetzner-infra-provider/internal/pkg/provider/resources"
)

const (
	machineRequestSetLabel = "omni-machine-request-set"

	// qemuGuestAgentExtension is required on Hetzner Cloud: like Proxmox,
	// Hetzner Cloud servers are KVM guests, and the qemu-guest-agent enables
	// features (e.g. accurate console/IP reporting) Hetzner's platform
	// expects to be able to talk to.
	qemuGuestAgentExtension = "siderolabs/qemu-guest-agent"

	// imageDownloadTokenTTL is how long the image factory URL must stay valid:
	// long enough to boot a rescue server and stream the image onto its disk.
	imageDownloadTokenTTL = time.Hour
)

// Provisioner implements the Hetzner Cloud Omni infra provider.
type Provisioner struct {
	hcloudClient *hcloud.Client
	imageCache   *imageCache
}

// NewProvisioner creates a new provisioner.
func NewProvisioner(hcloudClient *hcloud.Client, imagesClient *hcloudimages.Client) *Provisioner {
	return &Provisioner{
		hcloudClient: hcloudClient,
		imageCache:   newImageCache(hcloudClient, imagesClient),
	}
}

// ProvisionSteps implements infra.Provisioner.
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep("ensureImage", p.ensureImage),
		provision.NewStep("createServer", p.createServer),
		provision.NewStep("awaitServer", p.awaitServer),
		provision.NewStep("createVolumes", p.createVolumes),
	}
}

// schematicOptions returns the schematic options for a machine request.
func schematicOptions(data Data) []provision.SchematicOption {
	// WithoutConnectionParams is required here: the resulting image is shared and
	// reused by every machine built from this schematic/Talos version, so no
	// per-machine join secrets may be embedded in it. Join config is instead
	// delivered per-server via UserData in createServer.
	extensions := []string{qemuGuestAgentExtension}

	for _, extension := range data.Extensions {
		if extension == qemuGuestAgentExtension {
			continue
		}

		extensions = append(extensions, extension)
	}

	return []provision.SchematicOption{
		provision.WithoutConnectionParams(),
		provision.WithExtraExtensions(extensions...),
	}
}

// talosArch maps a Hetzner server architecture to the Talos architecture name.
func talosArch(arch hcloud.Architecture) string {
	if arch == hcloud.ArchitectureARM {
		return "arm64"
	}

	return "amd64"
}

func (p *Provisioner) ensureImage(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	if pctx.State.TypedSpec().Value.ImageId != 0 {
		return nil
	}

	var data Data

	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return err
	}

	serverType, _, err := p.hcloudClient.ServerType.GetByName(ctx, data.ServerType)
	if err != nil {
		return fmt.Errorf("failed to look up server type %q: %w", data.ServerType, err)
	}

	if serverType == nil {
		return fmt.Errorf("server type %q not found", data.ServerType)
	}

	location, err := p.resolveLocation(ctx, data.Location)
	if err != nil {
		return err
	}

	pctx.State.TypedSpec().Value.Architecture = string(serverType.Architecture)

	// The medium is fetched by hcloud-upload-image on a temporary rescue server,
	// not by this process, so ask for a self-contained URL that stays valid for
	// the whole (multi-minute) upload.
	media, err := pctx.EnsureInstallationMedia(ctx, logger, provision.MediaSpec{
		Kind:             imagefactory.InstallationMediaKindDisk,
		Platform:         "hcloud",
		Architecture:     talosArch(serverType.Architecture),
		Format:           "raw.xz",
		DownloadTokenTTL: imageDownloadTokenTTL,
		StandaloneURL:    true,
	}, schematicOptions(data)...)
	if err != nil {
		return fmt.Errorf("failed to resolve installation media: %w", err)
	}

	pctx.State.TypedSpec().Value.Schematic = media.SchematicID
	pctx.State.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()

	imageID, ok, err := p.imageCache.ensureImage(
		ctx, logger,
		media,
		pctx.State.TypedSpec().Value.TalosVersion,
		serverType.Architecture,
		location,
	)
	if err != nil {
		return err
	}

	if !ok {
		return provision.NewRetryInterval(15 * time.Second)
	}

	pctx.State.TypedSpec().Value.ImageId = imageID

	return nil
}

func (p *Provisioner) createServer(ctx context.Context, _ *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	if pctx.State.TypedSpec().Value.ServerId != 0 {
		return nil
	}

	var data Data

	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return err
	}

	if data.Location == "" {
		return fmt.Errorf("location is required in the machine class config")
	}

	if data.ServerType == "" {
		return fmt.Errorf("server_type is required in the machine class config")
	}

	location, err := p.resolveLocation(ctx, data.Location)
	if err != nil {
		return err
	}

	networks, err := p.resolveNetworks(ctx, data.Networks)
	if err != nil {
		return err
	}

	firewalls, err := p.resolveFirewalls(ctx, data.Firewalls)
	if err != nil {
		return err
	}

	sshKeys, err := p.resolveSSHKeys(ctx, data.SSHKeys)
	if err != nil {
		return err
	}

	var placementGroup *hcloud.PlacementGroup

	if data.PlacementGroup != "" {
		placementGroup, _, err = p.hcloudClient.PlacementGroup.Get(ctx, data.PlacementGroup)
		if err != nil {
			return fmt.Errorf("failed to look up placement group %q: %w", data.PlacementGroup, err)
		}

		if placementGroup == nil {
			return fmt.Errorf("placement group %q not found", data.PlacementGroup)
		}
	}

	machineRequestSet, _ := pctx.GetMachineRequestSetID()

	labels := map[string]string{}
	maps.Copy(labels, data.Labels)

	if machineRequestSet != "" {
		labels[machineRequestSetLabel] = machineRequestSet
	}

	result, _, err := p.hcloudClient.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name:       pctx.GetRequestID(),
		ServerType: &hcloud.ServerType{Name: data.ServerType},
		Image:      &hcloud.Image{ID: pctx.State.TypedSpec().Value.ImageId},
		Location:   location,
		UserData:   pctx.ConnectionParams.JoinConfig,
		Labels:     labels,
		Networks:   networks,
		Firewalls:  firewalls,
		SSHKeys:    sshKeys,

		PlacementGroup:   placementGroup,
		StartAfterCreate: new(true),
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: boolDefault(data.EnablePublicIPv4, true),
			EnableIPv6: boolDefault(data.EnablePublicIPv6, true),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	pctx.State.TypedSpec().Value.ServerId = result.Server.ID
	pctx.State.TypedSpec().Value.CreateActionId = result.Action.ID

	return provision.NewRetryInterval(5 * time.Second)
}

func (p *Provisioner) awaitServer(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	actionID := pctx.State.TypedSpec().Value.CreateActionId
	if actionID == 0 {
		return nil
	}

	err := p.checkActionStatus(ctx, actionID)

	if actionErr, ok := errors.AsType[hcloud.ActionError](err); ok {
		// The create action failed for good (e.g. resource_unavailable when the
		// location is out of capacity). Polling the same action again can never
		// succeed, so discard the half-created server and start over from
		// createServer on the next reconcile.
		logger.Warn("server creation failed, recreating",
			zap.Int64("server_id", pctx.State.TypedSpec().Value.ServerId),
			zap.String("code", actionErr.Code),
			zap.String("message", actionErr.Message),
		)

		if delErr := p.deleteServer(ctx, pctx.State.TypedSpec().Value.ServerId); delErr != nil {
			return fmt.Errorf("server creation failed (%w), and cleanup failed: %w", err, delErr)
		}

		pctx.State.TypedSpec().Value.ServerId = 0
		pctx.State.TypedSpec().Value.CreateActionId = 0

		return fmt.Errorf("server creation failed, will retry: %w", err)
	}

	if err != nil {
		return err
	}

	pctx.State.TypedSpec().Value.CreateActionId = 0

	return nil
}

// deleteServer deletes a server and waits for the deletion to finish. A server
// that is already gone is not an error.
func (p *Provisioner) deleteServer(ctx context.Context, serverID int64) error {
	if serverID == 0 {
		return nil
	}

	result, _, err := p.hcloudClient.Server.DeleteWithResult(ctx, &hcloud.Server{ID: serverID})
	if err != nil {
		if isAlreadyGoneErr(err) {
			return nil
		}

		return fmt.Errorf("failed to delete server %d: %w", serverID, err)
	}

	if result.Action != nil {
		if err = p.hcloudClient.Action.WaitFor(ctx, result.Action); err != nil {
			return fmt.Errorf("failed waiting for server %d deletion: %w", serverID, err)
		}
	}

	return nil
}

func (p *Provisioner) createVolumes(ctx context.Context, _ *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	var data Data

	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return err
	}

	if len(data.Volumes) == 0 {
		return nil
	}

	spec := pctx.State.TypedSpec().Value
	if len(spec.VolumeIds) >= len(data.Volumes) {
		return nil
	}

	server := &hcloud.Server{ID: spec.ServerId}

	for i := len(spec.VolumeIds); i < len(data.Volumes); i++ {
		volume := data.Volumes[i]

		opts := hcloud.VolumeCreateOpts{
			Name:      fmt.Sprintf("%s-%s", pctx.GetRequestID(), volume.Name),
			Size:      volume.Size,
			Server:    server,
			Automount: new(volume.Automount),
		}

		if volume.Format != "" {
			opts.Format = new(volume.Format)
		}

		result, _, err := p.hcloudClient.Volume.Create(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create volume %q: %w", volume.Name, err)
		}

		spec.VolumeIds = append(spec.VolumeIds, result.Volume.ID)

		if result.Action != nil {
			if err = p.hcloudClient.Action.WaitFor(ctx, result.Action); err != nil {
				return fmt.Errorf("failed to attach volume %q: %w", volume.Name, err)
			}
		}
	}

	return nil
}

// HealthCheck verifies connectivity to the Hetzner Cloud API.
// Results are surfaced in the Omni UI via infra.WithHealthCheckFunc.
func (p *Provisioner) HealthCheck(ctx context.Context) error {
	_, _, err := p.hcloudClient.Location.List(ctx, hcloud.LocationListOpts{})
	if err != nil {
		return fmt.Errorf("hetzner cloud API unreachable: %w", err)
	}

	return nil
}

// Deprovision implements infra.Provisioner.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, machine *resources.Machine, _ *infra.MachineRequest) error {
	spec := machine.TypedSpec().Value

	for _, volumeID := range spec.VolumeIds {
		if _, err := p.hcloudClient.Volume.Delete(ctx, &hcloud.Volume{ID: volumeID}); err != nil && !isAlreadyGoneErr(err) {
			return fmt.Errorf("failed to delete volume %d: %w", volumeID, err)
		}
	}

	if spec.ServerId == 0 {
		return nil
	}

	result, _, err := p.hcloudClient.Server.DeleteWithResult(ctx, &hcloud.Server{ID: spec.ServerId})
	if err != nil {
		if isAlreadyGoneErr(err) {
			logger.Info("server already gone, treating as deprovisioned", zap.Int64("server_id", spec.ServerId))

			return nil
		}

		return fmt.Errorf("failed to delete server: %w", err)
	}

	if result.Action != nil {
		if err = p.hcloudClient.Action.WaitFor(ctx, result.Action); err != nil {
			return fmt.Errorf("failed waiting for server deletion: %w", err)
		}
	}

	// The Image built for this schematic/Talos version is intentionally left in
	// place: it is a shared cache reused by future machines, not a per-machine
	// resource. Cleaning up unreferenced cached images is left as a follow-up.

	return nil
}

func (p *Provisioner) checkActionStatus(ctx context.Context, actionID int64) error {
	action, _, err := p.hcloudClient.Action.GetByID(ctx, actionID)
	if err != nil {
		return err
	}

	if action == nil {
		return fmt.Errorf("action %d not found", actionID)
	}

	switch action.Status {
	case hcloud.ActionStatusRunning:
		return provision.NewRetryInterval(5 * time.Second)
	case hcloud.ActionStatusSuccess:
		return nil
	case hcloud.ActionStatusError:
		return action.Error()
	default:
		return fmt.Errorf("unexpected action status %q", action.Status)
	}
}

func (p *Provisioner) resolveLocation(ctx context.Context, name string) (*hcloud.Location, error) {
	if name == "" {
		return nil, fmt.Errorf("location is required in the machine class config")
	}

	location, _, err := p.hcloudClient.Location.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to look up location %q: %w", name, err)
	}

	if location == nil {
		return nil, fmt.Errorf("location %q not found", name)
	}

	return location, nil
}

func (p *Provisioner) resolveNetworks(ctx context.Context, names []string) ([]*hcloud.Network, error) {
	if len(names) == 0 {
		return nil, nil
	}

	networks := make([]*hcloud.Network, 0, len(names))

	for _, name := range names {
		network, _, err := p.hcloudClient.Network.Get(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to look up network %q: %w", name, err)
		}

		if network == nil {
			return nil, fmt.Errorf("network %q not found", name)
		}

		networks = append(networks, network)
	}

	return networks, nil
}

func (p *Provisioner) resolveFirewalls(ctx context.Context, names []string) ([]*hcloud.ServerCreateFirewall, error) {
	if len(names) == 0 {
		return nil, nil
	}

	firewalls := make([]*hcloud.ServerCreateFirewall, 0, len(names))

	for _, name := range names {
		firewall, _, err := p.hcloudClient.Firewall.Get(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to look up firewall %q: %w", name, err)
		}

		if firewall == nil {
			return nil, fmt.Errorf("firewall %q not found", name)
		}

		firewalls = append(firewalls, &hcloud.ServerCreateFirewall{Firewall: *firewall})
	}

	return firewalls, nil
}

func (p *Provisioner) resolveSSHKeys(ctx context.Context, names []string) ([]*hcloud.SSHKey, error) {
	if len(names) == 0 {
		return nil, nil
	}

	sshKeys := make([]*hcloud.SSHKey, 0, len(names))

	for _, name := range names {
		sshKey, _, err := p.hcloudClient.SSHKey.Get(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to look up ssh key %q: %w", name, err)
		}

		if sshKey == nil {
			return nil, fmt.Errorf("ssh key %q not found", name)
		}

		sshKeys = append(sshKeys, sshKey)
	}

	return sshKeys, nil
}

// isAlreadyGoneErr reports whether err means the resource no longer exists in
// Hetzner Cloud (e.g. deleted out-of-band). On teardown such a resource has
// nothing left to clean up, so the caller treats it as already deprovisioned.
func isAlreadyGoneErr(err error) bool {
	return hcloud.IsError(err, hcloud.ErrorCodeNotFound)
}

func boolDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}

	return *v
}
