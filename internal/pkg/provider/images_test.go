// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"strings"
	"testing"
)

func TestImageCacheKey(t *testing.T) {
	got := imageCacheKey("sha256:abc", "v1.9.0", "x86")
	want := "sha256:abc/v1.9.0/x86"

	if got != want {
		t.Errorf("imageCacheKey() = %q, want %q", got, want)
	}

	// Different architectures must not collide.
	if imageCacheKey("s", "v1", "x86") == imageCacheKey("s", "v1", "arm") {
		t.Error("imageCacheKey() collided across architectures")
	}
}

func TestImageLabelSelector(t *testing.T) {
	got := imageLabelSelector("sha256:abc", "v1.9.0", "x86")
	want := "schematic=sha256:abc,talos-version=v1.9.0,arch=x86"

	if got != want {
		t.Errorf("imageLabelSelector() = %q, want %q", got, want)
	}
}

func TestHetznerLabelValue(t *testing.T) {
	// A Talos schematic ID is a 64-char sha256 hex digest -- one character
	// over Hetzner's 63-char label value limit (confirmed against the live
	// API: a 64-char value is rejected as invalid_input, 63 chars is fine).
	schematic64 := strings.Repeat("a", 64)

	got := hetznerLabelValue(schematic64)
	if len(got) != 63 {
		t.Errorf("hetznerLabelValue(64-char) length = %d, want 63", len(got))
	}

	if got != strings.Repeat("a", 63) {
		t.Errorf("hetznerLabelValue(64-char) = %q, want first 63 chars preserved", got)
	}

	short := "abc123"
	if got := hetznerLabelValue(short); got != short {
		t.Errorf("hetznerLabelValue(short) = %q, want unchanged %q", got, short)
	}

	exactly63 := strings.Repeat("b", 63)
	if got := hetznerLabelValue(exactly63); got != exactly63 {
		t.Errorf("hetznerLabelValue(63-char) = %q, want unchanged", got)
	}
}

func TestImageLabelSelectorTruncatesSchematic(t *testing.T) {
	schematic64 := strings.Repeat("a", 64)

	got := imageLabelSelector(schematic64, "1.9.0", "x86")
	want := "schematic=" + strings.Repeat("a", 63) + ",talos-version=1.9.0,arch=x86"

	if got != want {
		t.Errorf("imageLabelSelector() = %q, want %q", got, want)
	}
}

func TestTalosImageFactoryURL(t *testing.T) {
	u, err := talosImageFactoryURL("sha256:abc", "v1.9.0")
	if err != nil {
		t.Fatalf("talosImageFactoryURL() error = %v", err)
	}

	want := "https://factory.talos.dev/image/sha256:abc/v1.9.0/hcloud-amd64.raw.xz"
	if u.String() != want {
		t.Errorf("talosImageFactoryURL() = %q, want %q", u.String(), want)
	}
}
