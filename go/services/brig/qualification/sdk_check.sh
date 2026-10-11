#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
# Run inside the identified, nonroot candidate compiler SDK only.
set -eu
test "$(id -u)" = 65534
cp /opt/brig-sdk/packages.txt /opt/brig-sdk/libraries.sha256 /output/
cp /usr/share/doc/libargon2-1/copyright /output/argon2-copyright
cp /usr/share/doc/libssl3t64/copyright /output/openssl-copyright
cp /usr/share/doc/zlib1g/copyright /output/zlib-copyright
cp /usr/share/doc/libzstd1/copyright /output/zstd-copyright
cp /usr/share/doc/libc6/copyright /output/glibc-copyright
cp /usr/share/doc/libicu76/copyright /output/icu-copyright
cp /usr/share/doc/libstdc++6/copyright /output/libstdc++-copyright
python3 /source/go/check.py brig > /output/go-check.log 2>&1
cd /source/go/services/brig
go build -trimpath -buildvcs=false -tags=netgo,osusergo \
    -ldflags='-linkmode external -extldflags "-static -lz -lzstd"' \
    -o /output/brig ./cmd/brig
go test -c -trimpath -buildvcs=false -tags=netgo,osusergo \
    -ldflags='-linkmode external -extldflags "-static -lz -lzstd"' \
    -o /output/brig.test .
