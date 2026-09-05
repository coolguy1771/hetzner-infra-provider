// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/apricote/hcloud-upload-image/hcloudimages/v2"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"go.uber.org/zap"
)

const (
	imageFactoryBaseURL = "https://factory.talos.dev"

	labelSchematic    = "schematic"
	labelTalosVersion = "talos-version"
	labelArch         = "arch"

	// hetznerLabelValueMaxLen is Hetzner Cloud's hard limit on label values.
	// A Talos schematic ID is a 64-character sha256 hex digest, one character
	// over this limit, so it must be truncated before use as a label value
	// (confirmed against the live API: a 64-char value is rejected with
	// "invalid label_selector: value contains invalid characters or is
	// malformed", a 63-char value is accepted).
	hetznerLabelValueMaxLen = 63
)

// hetznerLabelValue truncates v to fit Hetzner's label value length limit.
// Used only for the schematic ID: 63 hex characters (252 bits) is still
// astronomically collision-resistant for a cache key, and the full schematic
// is preserved elsewhere (image Description, in-memory cache key).
func hetznerLabelValue(v string) string {
	if len(v) > hetznerLabelValueMaxLen {
		return v[:hetznerLabelValueMaxLen]
	}

	return v
}

// imageBuild tracks an in-flight (or finished) image upload for a given
// schematic/talos-version/architecture key, so that concurrently provisioning
// machines from the same MachineRequestSet trigger exactly one upload instead
// of racing to build the same image.
type imageBuild struct {
	done  chan struct{}
	image *hcloud.Image
	err   error
}

// imageCache resolves a Talos schematic/version/architecture to a cached (or
// freshly built) Hetzner Cloud snapshot image.
type imageCache struct {
	hcloudClient *hcloud.Client
	imagesClient *hcloudimages.Client

	mu     sync.Mutex
	builds map[string]*imageBuild
}

func newImageCache(hcloudClient *hcloud.Client, imagesClient *hcloudimages.Client) *imageCache {
	return &imageCache{
		hcloudClient: hcloudClient,
		imagesClient: imagesClient,
		builds:       make(map[string]*imageBuild),
	}
}

func imageCacheKey(schematic, talosVersion, arch string) string {
	return schematic + "/" + talosVersion + "/" + arch
}

func imageLabelSelector(schematic, talosVersion, arch string) string {
	return fmt.Sprintf("%s=%s,%s=%s,%s=%s",
		labelSchematic, hetznerLabelValue(schematic),
		labelTalosVersion, talosVersion,
		labelArch, arch,
	)
}

func talosImageFactoryURL(schematic, talosVersion string) (*url.URL, error) {
	u, err := url.Parse(imageFactoryBaseURL)
	if err != nil {
		return nil, err
	}

	return u.JoinPath("image", schematic, talosVersion, "hcloud-amd64.raw.xz"), nil
}

// ensureImage returns the Hetzner Cloud snapshot image ID for the given
// schematic/talos-version/architecture, building it if it does not exist yet.
// While a build is in flight it returns ok=false so the caller can retry later
// instead of blocking on the multi-minute upload.
func (c *imageCache) ensureImage(ctx context.Context, logger *zap.Logger, schematic, talosVersion string, arch hcloud.Architecture, location *hcloud.Location) (imageID int64, ok bool, err error) {
	key := imageCacheKey(schematic, talosVersion, string(arch))

	images, err := c.hcloudClient.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: imageLabelSelector(schematic, talosVersion, string(arch)),
		},
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
	})
	if err != nil {
		return 0, false, fmt.Errorf("failed to list images: %w", err)
	}

	for _, image := range images {
		if image.Status == hcloud.ImageStatusAvailable {
			return image.ID, true, nil
		}
	}

	c.mu.Lock()

	build, inFlight := c.builds[key]
	if !inFlight {
		build = &imageBuild{done: make(chan struct{})}
		c.builds[key] = build

		go c.runBuild(build, schematic, talosVersion, arch, location)
	}

	c.mu.Unlock()

	select {
	case <-build.done:
	default:
		logger.Info("image build in progress", zap.String("schematic", schematic), zap.String("talos_version", talosVersion))

		return 0, false, nil
	}

	c.mu.Lock()
	delete(c.builds, key)
	c.mu.Unlock()

	if build.err != nil {
		return 0, false, fmt.Errorf("failed to build image: %w", build.err)
	}

	return build.image.ID, true, nil
}

func (c *imageCache) runBuild(build *imageBuild, schematic, talosVersion string, arch hcloud.Architecture, location *hcloud.Location) {
	defer close(build.done)

	ctx := context.Background()

	imageURL, err := talosImageFactoryURL(schematic, talosVersion)
	if err != nil {
		build.err = err

		return
	}

	build.image, build.err = c.imagesClient.Upload(ctx, hcloudimages.UploadOptions{
		WriteOptions: hcloudimages.WriteOptions{
			ImageURL:         imageURL,
			ImageCompression: hcloudimages.CompressionXZ,
		},
		Architecture: arch,
		Location:     location,
		Description:  hcloud.Ptr(fmt.Sprintf("Talos %s (%s)", talosVersion, schematic)),
		Labels: map[string]string{
			labelSchematic:    hetznerLabelValue(schematic),
			labelTalosVersion: talosVersion,
			labelArch:         string(arch),
		},
	})
}
