# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

FROM --platform=${BUILDPLATFORM} golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG SHA=none

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags "-s -w -X github.com/coolguy1771/hetzner-infra-provider/internal/version.Tag=${VERSION} -X github.com/coolguy1771/hetzner-infra-provider/internal/version.SHA=${SHA}" \
    -o /omni-infra-provider-hetzner \
    ./cmd/omni-infra-provider-hetzner

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=build /omni-infra-provider-hetzner /omni-infra-provider-hetzner

ENTRYPOINT ["/omni-infra-provider-hetzner"]
