# SPDX-License-Identifier: AGPL-3.0-only
# Candidate-only compiler SDK. Existing Haskell and e2e images are unchanged.
FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
RUN apt-get update \
    && apt-get install --no-install-recommends -y gcc libc6-dev libargon2-dev libssl-dev python3 ca-certificates \
    && mkdir -p /opt/brig-sdk \
    && dpkg-query -W > /opt/brig-sdk/packages.txt \
    && sha256sum /usr/lib/x86_64-linux-gnu/libargon2.a /usr/lib/x86_64-linux-gnu/libcrypto.a > /opt/brig-sdk/libraries.sha256 \
    && rm -rf /var/lib/apt/lists/*
USER 65534:65534
ENV HOME=/tmp TMPDIR=/tmp GOTOOLCHAIN=local GOWORK=off
