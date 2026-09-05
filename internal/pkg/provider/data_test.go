// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestDataYAMLRoundTrip(t *testing.T) {
	input := `
server_type: cx22
location: fsn1
labels:
  env: prod
networks:
  - my-net
firewalls:
  - allow-k8s
placement_group: my-pg
ssh_keys:
  - break-glass
enable_public_ipv4: false
enable_public_ipv6: true
extensions:
  - siderolabs/util-linux-tools
volumes:
  - name: data
    size: 100
    format: ext4
    automount: true
`

	var data Data

	if err := yaml.Unmarshal([]byte(input), &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if data.ServerType != "cx22" {
		t.Errorf("ServerType = %q, want cx22", data.ServerType)
	}

	if data.Location != "fsn1" {
		t.Errorf("Location = %q, want fsn1", data.Location)
	}

	if data.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %q, want prod", data.Labels["env"])
	}

	if len(data.Networks) != 1 || data.Networks[0] != "my-net" {
		t.Errorf("Networks = %v, want [my-net]", data.Networks)
	}

	if len(data.Firewalls) != 1 || data.Firewalls[0] != "allow-k8s" {
		t.Errorf("Firewalls = %v, want [allow-k8s]", data.Firewalls)
	}

	if data.PlacementGroup != "my-pg" {
		t.Errorf("PlacementGroup = %q, want my-pg", data.PlacementGroup)
	}

	if len(data.SSHKeys) != 1 || data.SSHKeys[0] != "break-glass" {
		t.Errorf("SSHKeys = %v, want [break-glass]", data.SSHKeys)
	}

	if data.EnablePublicIPv4 == nil || *data.EnablePublicIPv4 {
		t.Errorf("EnablePublicIPv4 = %v, want false", data.EnablePublicIPv4)
	}

	if data.EnablePublicIPv6 == nil || !*data.EnablePublicIPv6 {
		t.Errorf("EnablePublicIPv6 = %v, want true", data.EnablePublicIPv6)
	}

	if len(data.Extensions) != 1 || data.Extensions[0] != "siderolabs/util-linux-tools" {
		t.Errorf("Extensions = %v", data.Extensions)
	}

	if len(data.Volumes) != 1 {
		t.Fatalf("Volumes = %v, want 1 entry", data.Volumes)
	}

	vol := data.Volumes[0]
	if vol.Name != "data" || vol.Size != 100 || vol.Format != "ext4" || !vol.Automount {
		t.Errorf("Volumes[0] = %+v", vol)
	}
}

func TestDataYAMLDefaults(t *testing.T) {
	var data Data

	if err := yaml.Unmarshal([]byte("server_type: cx22\nlocation: fsn1\n"), &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if data.EnablePublicIPv4 != nil {
		t.Errorf("EnablePublicIPv4 = %v, want nil (unset)", data.EnablePublicIPv4)
	}

	if data.EnablePublicIPv6 != nil {
		t.Errorf("EnablePublicIPv6 = %v, want nil (unset)", data.EnablePublicIPv6)
	}

	if len(data.Volumes) != 0 {
		t.Errorf("Volumes = %v, want empty", data.Volumes)
	}
}
