// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package config describes the connection settings for the Hetzner infra provider.
package config

// Config describes Hetzner provider configuration.
type Config struct {
	Hetzner Hetzner `yaml:"hetzner"`
}

// Hetzner is the config for accessing the Hetzner Cloud API.
type Hetzner struct {
	// Token is the Hetzner Cloud API token. If empty, falls back to the
	// HCLOUD_TOKEN environment variable.
	Token string `yaml:"token,omitempty"`

	// Endpoint overrides the Hetzner Cloud API endpoint, mainly useful for testing
	// against a mock/local API.
	Endpoint string `yaml:"endpoint,omitempty"`
}
