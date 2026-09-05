// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package version contains build-time version information, set via -ldflags.
package version

// Name declares the project name.
const Name = "hetzner-infra-provider"

var (
	// Tag declares the project git tag, set via -ldflags at build time.
	Tag = "dev"
	// SHA declares the project git SHA, set via -ldflags at build time.
	SHA = "none"
)
