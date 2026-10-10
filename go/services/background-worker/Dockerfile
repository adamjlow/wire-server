# Optional whole-service image; Haskell deployments remain default.
FROM scratch
COPY --chmod=0755 background-worker /background-worker
COPY --chmod=0755 openssl /bin/openssl
COPY ca-bundle.crt /etc/ssl/certs/ca-bundle.crt
COPY licenses /usr/share/licenses/background-worker
ENV PATH=/bin SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt
USER 65534:65534
ENTRYPOINT ["/background-worker"]
