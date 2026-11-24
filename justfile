# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

module := "github.com/coolguy1771/hetzner-infra-provider"
binary := "omni-infra-provider-hetzner"
version := `git describe --tags --always --dirty --match 'v[0-9]*' 2>/dev/null || echo dev`
sha := `git rev-parse --short HEAD 2>/dev/null || echo none`
ldflags := "-s -w -X " + module + "/internal/version.Tag=" + version + " -X " + module + "/internal/version.SHA=" + sha

# List available recipes.
default:
    @just --list

# Generate, lint, test and build.
all: generate lint test build

# Regenerate protobuf code.
generate:
    protoc -I. --go_out=. --go_opt=paths=source_relative api/specs/specs.proto

# Build the provider binary for the host platform into _out/.
build:
    CGO_ENABLED=0 go build -trimpath -ldflags "{{ ldflags }}" -o _out/{{ binary }} ./cmd/{{ binary }}

# Run golangci-lint.
lint:
    golangci-lint run ./...

# Run tests with the race detector.
test:
    go test -race -count=1 ./...

# Validate the GoReleaser config.
release-check:
    goreleaser check

# Build all release artifacts and images locally without publishing (output in dist/).
snapshot:
    goreleaser release --snapshot --clean

# Print the version the next release would get (empty: nothing to release).
next-version:
    @hack/next-version.sh

# Remove build output.
clean:
    rm -rf _out dist
