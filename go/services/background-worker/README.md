# Optional Go background-worker (under construction)

This module is an early runnable slice, not an available service replacement.
Haskell remains the supported default. Both original configuration files and CLI
paths are accepted. Health/readiness and the federation-notification consumer are
implemented with verified RabbitMQ TLS, paginated queue discovery, single-active
quorum queues, raw-body forwarding over internal HTTP2, version selection,
ACK/reject/retry and cancellation without acknowledging unsent messages.

The DLQ watcher, Rabbit group-sync jobs, PostgreSQL scheduled adminless/meetings
workers and all seven migration families are still being implemented. Their
readiness remains false; enabled migrations fail startup explicitly. This state
must not be selected for a deployment. Final acceptance remains the whole service
in the frozen reference contract, original six-service tests and real durable
Haskell-Go-Haskell switchback. There is no final compatibility or image claim.

Use pinned Go1.26.9 with `GOTOOLCHAIN=local GOWORK=off`. Build the command at
`./cmd/background-worker`; use `CGO_ENABLED=0` for the Nix Docker harness. Supply
Rabbit credentials using the existing `RABBITMQ_USERNAME` and `RABBITMQ_PASSWORD`
environment variables. Errors and diagnostics do not print credentials or job
payloads. Inherited queue-blockage/drop behaviours are documented in the contract.
