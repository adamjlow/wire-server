# Optional whole-service image; existing Haskell deployments remain default.
FROM scratch
COPY --chmod=0755 spar /spar
COPY ca-bundle.crt /etc/ssl/certs/ca-bundle.crt
COPY licenses /usr/share/licenses/spar
COPY source /usr/share/source/spar
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt
USER 65534:65534
ENTRYPOINT ["/spar"]
