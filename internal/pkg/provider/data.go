// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

// Data is the provider custom machine config (MachineClass config).
type Data struct {
	// PlacementGroup attaches the server to an existing Hetzner Cloud placement group by name.
	PlacementGroup string `yaml:"placement_group,omitempty"`
	// ServerType is the Hetzner Cloud server type, e.g. "cx22". Required.
	ServerType string `yaml:"server_type"`
	// Location is the Hetzner Cloud datacenter location, e.g. "fsn1". Required.
	Location string `yaml:"location"`
	// EnablePublicIPv4 controls whether the server gets a public IPv4 address. Defaults to true.
	EnablePublicIPv4 *bool `yaml:"enable_public_ipv4,omitempty"`
	// EnablePublicIPv6 controls whether the server gets a public IPv6 address. Defaults to true.
	EnablePublicIPv6 *bool             `yaml:"enable_public_ipv6,omitempty"`
	Labels           map[string]string `yaml:"labels,omitempty"`
	// Networks are existing Hetzner Cloud private network names (or numeric IDs) to attach.
	Networks []string `yaml:"networks,omitempty"`
	// Firewalls are existing Hetzner Cloud firewall names (or numeric IDs) to attach.
	Firewalls []string `yaml:"firewalls,omitempty"`
	// SSHKeys are existing Hetzner Cloud SSH key names (or numeric IDs) attached for
	// break-glass access. Talos itself never uses SSH.
	SSHKeys []string `yaml:"ssh_keys,omitempty"`
	// Extensions are extra Talos system extensions baked into the schematic.
	Extensions []string `yaml:"extensions,omitempty"`
	// Volumes are additional Hetzner Cloud volumes created and attached to the server.
	Volumes []Volume `yaml:"volumes,omitempty"`
}

// Volume represents an additional Hetzner Cloud volume to create and attach.
type Volume struct {
	// Name is used as the Hetzner Cloud volume name, suffixed with the request ID to
	// keep it unique across machines.
	Name   string `yaml:"name"`
	Format string `yaml:"format,omitempty"`
	// Size is the volume size in GB.
	Size      int  `yaml:"size"`
	Automount bool `yaml:"automount,omitempty"`
}
