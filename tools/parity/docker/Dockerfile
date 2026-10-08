# The publisher bundle supplies compiled Haskell tests/services and their Nix
# runtime closures. No GHC, Nix or backend package is installed on the host.
FROM quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27
USER 0
COPY entrypoint.sh runner.py runtime.py harness.py e2e-tests.json gundeck-tests.json /opt/wire-e2e/
RUN chmod 0555 /opt/wire-e2e/entrypoint.sh /opt/wire-e2e/runner.py && chmod 0444 /opt/wire-e2e/e2e-tests.json /opt/wire-e2e/gundeck-tests.json
USER 65534:65534
WORKDIR /tmp
ENTRYPOINT ["/bin/bash", "/opt/wire-e2e/entrypoint.sh"]
CMD ["preflight"]
