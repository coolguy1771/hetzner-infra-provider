# Omni Infrastructure Provider for Hetzner Cloud

Automatically provisions Talos nodes as [Hetzner Cloud](https://www.hetzner.com/cloud)
servers, managed through [Omni](https://www.siderolabs.com/platform/saas-for-kubernetes/).

Built against the Omni
[infrastructure provider guide](https://docs.siderolabs.com/omni/infrastructure-and-extensions/writing-infrastructure-providers)
using [`client/pkg/infra`](https://github.com/siderolabs/omni/tree/main/client/pkg/infra).

## How it works

Hetzner Cloud has no direct image-upload API, so the provider builds a Talos
snapshot image once per (schematic, Talos version, architecture) by booting a
temporary server into Hetzner's rescue system, writing the
[Talos Image Factory](https://factory.talos.dev)'s `hcloud-amd64.raw.xz` disk image
onto it, snapshotting the disk, and deleting the temporary server (via
[`apricote/hcloud-upload-image`](https://github.com/apricote/hcloud-upload-image)).
That snapshot is cached and reused by every machine built from the same
schematic/version — it is not rebuilt per machine. Each server then gets its
own Talos join config injected through Hetzner's cloud-init-free `user_data`
field, which Talos's `hcloud` platform reads directly from the metadata service.

## Requirements

- A Hetzner Cloud project and API token with read/write access.
- An Omni account and an infrastructure provider key.
- Network connectivity from the provider to both the Hetzner Cloud API and your
  Omni instance.

## Running the Infrastructure Provider

Create a configuration file for the provider (or just set `HCLOUD_TOKEN` and skip
this):

```yaml
hetzner:
  token: "<hetzner-cloud-api-token>"
```

### Using Docker

```bash
docker run -it -d \
  -v ./config.yaml:/config.yaml \
  ghcr.io/coolguy1771/hetzner-infra-provider \
  --config-file /config.yaml \
  --omni-api-endpoint https://<account-name>.omni.siderolabs.io/ \
  --omni-service-account-key <infra-provider-key>
```

> **Note:** `--omni-service-account-key` expects an *infra provider key*, not a
> regular Omni service account key.

### Using Docker Compose

```yaml
services:
  omni-infra-provider-hetzner:
    image: ghcr.io/coolguy1771/hetzner-infra-provider
    volumes:
      - ./config.yaml:/config.yaml
    command: >
      --config-file /config.yaml
      --omni-api-endpoint https://<account-name>.omni.siderolabs.io/
      --omni-service-account-key <infra-provider-key>
    environment:
      - HCLOUD_TOKEN=<hetzner-cloud-api-token>
    restart: unless-stopped
```

### Using the executable

```bash
just build
_out/omni-infra-provider-hetzner \
  --omni-api-endpoint https://<account-name>.omni.siderolabs.io/ \
  --omni-service-account-key <infra-provider-key>
```

(`HCLOUD_TOKEN` env var used in place of `--config-file` here.)

## Creating a Machine Class for Auto Provision

```yaml
apiVersion: infrastructure.omni.siderolabs.io/v1alpha1
kind: MachineClass
metadata:
  name: hetzner-auto
spec:
  type: auto-provision
  provider: hetzner
  config:
    server_type: cx22
    location: fsn1
```

Use it to scale a cluster:

```yaml
spec:
  machineClass: hetzner-auto
  replicas: 3
```

Scaling up provisions new Hetzner Cloud servers; scaling down deletes them (and
any additional volumes they were given — the cached snapshot image is left in
place for future machines).

## Machine Class Options

| Field                 | Required | Description                                                                 |
| --------------------- | -------- | ----------------------------------------------------------------------------- |
| `server_type`          | yes      | Hetzner Cloud server type, e.g. `cx22`                                       |
| `location`             | yes      | Hetzner Cloud datacenter, e.g. `fsn1`, `nbg1`, `hel1`, `ash`, `hil`          |
| `labels`               | no       | Extra labels to set on the server                                            |
| `networks`             | no       | Existing private network names/IDs to attach                                 |
| `firewalls`            | no       | Existing firewall names/IDs to attach                                        |
| `placement_group`      | no       | Existing placement group name/ID to attach                                   |
| `ssh_keys`             | no       | Existing SSH key names/IDs for break-glass access (Talos itself never uses SSH) |
| `enable_public_ipv4`   | no       | Assign a public IPv4 address. Default `true`                                  |
| `enable_public_ipv6`   | no       | Assign a public IPv6 address. Default `true`                                  |
| `extensions`           | no       | Extra Talos system extensions baked into the schematic, in addition to `siderolabs/qemu-guest-agent` (always included — Hetzner Cloud servers are KVM guests and expect it) |
| `volumes`              | no       | Additional volumes to create and attach; see below                           |

### Additional Volumes

```yaml
config:
  ...
  volumes:
    - name: data
      size: 100
      format: ext4
      automount: true
```

Volumes are created after the server exists and are deleted along with it when
the machine is deprovisioned (Hetzner does not delete attached volumes
automatically).

### Private Networks and Firewalls

```yaml
config:
  ...
  networks:
    - my-private-network
  firewalls:
    - allow-k8s-api
```

Reference existing Hetzner Cloud resources by name or numeric ID; they are
resolved and attached at server creation time.

## Design Notes

Follows the Omni
[writing infrastructure providers](https://docs.siderolabs.com/omni/infrastructure-and-extensions/writing-infrastructure-providers)
guidance:

- Shared snapshot images per schematic/Talos version/arch (not one image per machine).
- `provision.WithoutConnectionParams` plus `infra.WithEncodeRequestIDsIntoTokens` so join
  secrets stay out of the cached image; V2 tokens map machines to requests on join.
- Join config delivered per server via Hetzner `user_data` (`ConnectionParams.JoinConfig`).
- Long-running Hetzner actions polled with `provision.NewRetryInterval` instead of blocking.
- Omni UI health via `infra.WithHealthCheckFunc` (Hetzner Cloud API reachability).
- `SetMachineUUID` is not used: token encoding already maps requests to machines.

State lives in Omni under the provider namespace; the process needs no local database.

## Recovering From an Interrupted Image Build

Building the cached snapshot image involves a temporary server that is normally
cleaned up automatically. If the provider is killed mid-build, run:

```bash
omni-infra-provider-hetzner cleanup --config-file config.yaml
```

to remove any leftover temporary servers/SSH keys.

## Development

Tasks are run with [just](https://github.com/casey/just) (`just --list`):
`just build`, `just test`, `just lint`, `just generate`, and `just snapshot`
(a full local GoReleaser build into `dist/`, nothing published).

## Releases

Releases are cut by [release-please](https://github.com/googleapis/release-please)
from [Conventional Commits](https://www.conventionalcommits.org/) on `main`
(`feat:`, `fix:`, `deps:`, …). It keeps a release PR open; merging it tags the
release, and [GoReleaser](https://goreleaser.com) then publishes:

- `linux`/`darwin` × `amd64`/`arm64` tarballs and raw binaries
  (`omni-infra-provider-hetzner_<os>_<arch>`)
- `deb`, `rpm`, `apk` and Arch Linux packages that install a hardened systemd
  unit; set credentials in `/etc/omni-infra-provider-hetzner/env`, then
  `systemctl enable --now omni-infra-provider-hetzner`
- SPDX SBOMs for every archive and package, and `checksums.txt`
- multi-arch `scratch` images (non-root) at
  `ghcr.io/coolguy1771/hetzner-infra-provider` (`X.Y.Z`, `X.Y`, `latest`) with
  SBOM attestations
- keyless [cosign](https://github.com/sigstore/cosign) signatures for the
  checksums and the images

Verify a release:

```bash
IDENTITY='https://github.com/coolguy1771/hetzner-infra-provider/.github/workflows/release.yml@refs/heads/main'
ISSUER='https://token.actions.githubusercontent.com'

cosign verify-blob --certificate-identity "$IDENTITY" --certificate-oidc-issuer "$ISSUER" \
  --bundle checksums.txt.sigstore.json checksums.txt
sha256sum --check --ignore-missing checksums.txt

cosign verify ghcr.io/coolguy1771/hetzner-infra-provider:<version> \
  --certificate-identity "$IDENTITY" --certificate-oidc-issuer "$ISSUER"
```
