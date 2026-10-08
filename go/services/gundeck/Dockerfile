# Built locally from a clean pinned Go binary and qualified reference CA bundle.
FROM scratch
COPY gundeck /gundeck
COPY gundeck-prestop /bin/sh
COPY ca-bundle.crt /etc/ssl/certs/ca-bundle.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt
ENV PATH=/bin
USER 65534:65534
ENTRYPOINT ["/gundeck"]
