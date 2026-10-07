# Built by build_image.py from a clean pinned Go binary and the reference CA bundle.
FROM scratch
COPY proxy /proxy
COPY ca-bundle.crt /etc/ssl/certs/ca-bundle.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt
USER 65534:65534
ENTRYPOINT ["/proxy"]
