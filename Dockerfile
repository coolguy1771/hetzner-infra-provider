# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at http://mozilla.org/MPL/2.0/.

# Built by GoReleaser (dockers_v2): the static binaries are cross-compiled
# beforehand and placed in the build context under $TARGETPLATFORM/. Build
# locally with `just snapshot`.

# Only used to source the CA bundle (the provider talks TLS to Omni, the
# Hetzner Cloud API and the image factory); nothing from it ships otherwise.
FROM alpine:3.24 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/omni-infra-provider-hetzner /omni-infra-provider-hetzner

USER 65532:65532

ENTRYPOINT ["/omni-infra-provider-hetzner"]
