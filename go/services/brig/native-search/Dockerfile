# SPDX-License-Identifier: AGPL-3.0-only
# Extend the identified local compiler SDK; qualification records its exact
# image ID before building. No Haskell or original test image is changed.
ARG BRIG_CRYPTO_SDK=wire-migration-brig-crypto-sdk:20261010-static
FROM ${BRIG_CRYPTO_SDK}
USER 0:0
RUN apt-get update \
    && apt-get install --no-install-recommends -y libicu-dev=76.1-4 g++ \
    && mkdir -p /opt/brig-cxx/package \
    && printf '%s\n' 'deb https://deb.debian.org/debian sid main' > /etc/apt/sources.list.d/brig-cxx.list \
    && apt-get update \
    && cd /opt/brig-cxx/package \
    && apt-get download libstdc++-14-dev=14.4.0-3 \
    && sha256sum *.deb > /opt/brig-sdk/cxx-package.sha256 \
    && dpkg-deb --extract libstdc++-14-dev_14.4.0-3_amd64.deb /opt/brig-cxx \
    && dpkg-deb --field libstdc++-14-dev_14.4.0-3_amd64.deb Package Version Source > /opt/brig-sdk/cxx-package.txt \
    && rm /etc/apt/sources.list.d/brig-cxx.list \
    && dpkg-query -W > /opt/brig-sdk/packages.txt \
    && sha256sum /usr/lib/x86_64-linux-gnu/libargon2.a /usr/lib/x86_64-linux-gnu/libcrypto.a /usr/lib/x86_64-linux-gnu/libz.a /usr/lib/x86_64-linux-gnu/libzstd.a /usr/lib/x86_64-linux-gnu/libicui18n.a /usr/lib/x86_64-linux-gnu/libicuuc.a /usr/lib/x86_64-linux-gnu/libicudata.a /opt/brig-cxx/usr/lib/gcc/x86_64-linux-gnu/14/libstdc++.a > /opt/brig-sdk/libraries.sha256 \
    && rm -rf /var/lib/apt/lists/*
USER 65534:65534
ENV LIBRARY_PATH=/opt/brig-cxx/usr/lib/gcc/x86_64-linux-gnu/14
