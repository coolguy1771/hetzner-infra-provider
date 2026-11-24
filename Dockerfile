# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

# Built by GoReleaser (dockers_v2): the binaries are cross-compiled beforehand
# and placed in the build context under $TARGETPLATFORM/. Build locally with
# `just snapshot`.
FROM alpine:3.24

RUN apk add --no-cache ca-certificates

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/omni-infra-provider-hetzner /omni-infra-provider-hetzner

ENTRYPOINT ["/omni-infra-provider-hetzner"]
