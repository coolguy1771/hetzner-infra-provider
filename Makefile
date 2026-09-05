# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

MODULE := github.com/coolguy1771/hetzner-infra-provider
IMAGE  := ghcr.io/coolguy1771/hetzner-infra-provider
VERSION ?= $(shell git describe --tag --always --dirty --match v[0-9]\* 2> /dev/null || echo dev)
SHA     ?= $(shell git rev-parse --short HEAD 2> /dev/null || echo none)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Tag=$(VERSION) \
	-X $(MODULE)/internal/version.SHA=$(SHA)

.PHONY: all
all: generate lint test build

.PHONY: generate
generate:
	protoc -I. --go_out=. --go_opt=paths=source_relative api/specs/specs.proto

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o _out/omni-infra-provider-hetzner ./cmd/omni-infra-provider-hetzner

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: docker
docker:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg SHA=$(SHA) \
		-t $(IMAGE):$(VERSION) \
		.

.PHONY: clean
clean:
	rm -rf _out
