# Optional local image; original Haskell deployments remain the default.
FROM scratch
COPY federator /federator
COPY ca-bundle.crt /etc/ssl/certs/ca-bundle.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt
USER 65534:65534
ENTRYPOINT ["/federator"]
