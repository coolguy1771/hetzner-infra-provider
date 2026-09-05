// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements the Hetzner Cloud infra provider core.
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/apricote/hcloud-upload-image/hcloudimages/v2"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
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
		provision.NewStep("createSchematic", p.createSchematic),
		provision.NewStep("ensureImage", p.ensureImage),
		provision.NewStep("createServer", p.createServer),
		provision.NewStep("awaitServer", p.awaitServer),
		provision.NewStep("createVolumes", p.createVolumes),
	}
}

func (p *Provisioner) createSchematic(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	if pctx.State.TypedSpec().Value.Schematic != "" {
		return nil
	}

	var data Data

	if err := pctx.UnmarshalProviderData(&data); err != nil {
		return err
	}

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

	opts := []provision.SchematicOption{
		provision.WithoutConnectionParams(),
		provision.WithExtraExtensions(extensions...),
	}

	schematic, err := pctx.GenerateSchematicID(ctx, logger, opts...)
	if err != nil {
		return err
	}

	pctx.State.TypedSpec().Value.Schematic = schematic
	pctx.State.TypedSpec().Value.TalosVersion = pctx.GetTalosVersion()

	return nil
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

	imageID, ok, err := p.imageCache.ensureImage(
		ctx, logger,
		pctx.State.TypedSpec().Value.Schematic,
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
	for k, v := range data.Labels {
		labels[k] = v
	}

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
		StartAfterCreate: hcloud.Ptr(true),
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

func (p *Provisioner) awaitServer(ctx context.Context, _ *zap.Logger, pctx provision.Context[*resources.Machine]) error {
	actionID := pctx.State.TypedSpec().Value.CreateActionId
	if actionID == 0 {
		return nil
	}

	if err := p.checkActionStatus(ctx, actionID); err != nil {
		return err
	}

	pctx.State.TypedSpec().Value.CreateActionId = 0

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
			Automount: hcloud.Ptr(volume.Automount),
		}

		if volume.Format != "" {
			opts.Format = hcloud.Ptr(volume.Format)
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
