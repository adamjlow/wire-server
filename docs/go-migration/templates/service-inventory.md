# <service> inventory

Reference commit: <full SHA>; author/task: <id>; inspected: <date>.
This is an inventory, not an approved compatibility contract.

## Boundary and entry points

List executable, public/internal/federation routes, API versions, WebSockets,
scheduled jobs and queue consumers. Cite source files/symbols at the reference SHA.
Trace behaviour into shared libraries; distinguish this service from its peers.

## Dependencies and effects

Map callers, callees, queues, stores, schema ownership, transactions, consistency,
retry/idempotency and external side effects. Record mixed-version constraints.

## Security and observable behaviour

Map authentication/authorization, validation, rate/resource limits, serialization,
errors and negative paths. Use synthetic examples. Mark unknowns explicitly;
do not treat current security defects as permission to reproduce them.

## Operations

List config defaults/precedence, secrets interfaces, flags, ports, DNS/discovery,
health/readiness, logs/metrics/traces, shutdown, resource limits, deployment/image
build and rollback dependencies. Link relevant charts, Nix and nginz config.
Identify upstream-owned files and existing build/install/upgrade paths that must
remain unchanged by default. Map transitive upstream changes that could invalidate
Go compatibility, and existing data/version limits on switching implementations.

## Existing evidence and gaps

List unit/golden/integration suites and their coverage, exact permitted commands,
fixtures and environment requirements. Distinguish inspected from executed.
Identify missing scenarios, blockers and proposed bounded follow-up tasks.
