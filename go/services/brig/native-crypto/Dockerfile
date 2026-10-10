# SPDX-License-Identifier: AGPL-3.0-only
# Candidate-only compiler SDK. Existing Haskell and e2e images are unchanged.
FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update \
    && apt-get install --no-install-recommends -y gcc libc6-dev libargon2-dev libssl-dev python3 ca-certificates \
    && mkdir -p /opt/brig-sdk \
    && dpkg-query -W > /opt/brig-sdk/packages.txt \
    && sha256sum /usr/lib/x86_64-linux-gnu/libargon2.a /usr/lib/x86_64-linux-gnu/libcrypto.a > /opt/brig-sdk/libraries.sha256 \
    && rm -rf /var/lib/apt/lists/*
USER 65534:65534
ENV HOME=/tmp TMPDIR=/tmp GOTOOLCHAIN=local GOWORK=off
