# Optional Go cannon candidate

Work in progress against the frozen cannon contract. Haskell remains supported
and the default; this module does not yet claim whole-service parity/availability.
Go 1.26.8, existing upstream YAML and required RabbitMQ credential environment.

Build from this module with `go build -o cannon ./cmd/cannon`; run with `-c` or
`--config-file`, default `/etc/wire/cannon/conf/cannon.yaml`. Backend dependencies
remain existing Cassandra, RabbitMQ and gundeck processes. There is no schema or
queue-layout migration. TLS verification remains required; the inherited explicit
insecureSkipVerifyTls=true configuration is deliberately unqualified and rejected
under repository policy. No production config in the qualification matrix needs
that bypass; resolve its contract disposition separately rather than silently
advertising compatibility for it.

Current runnable behavior: classic await/websocket aliases, presence registration,
raw binary push, ordered typed bulk responses, generation-safe cleanup, pongs,
consumable events with prefetch500, typed ack/full-sync/sync marker, Cassandra
missed-notification recovery, temporary/client queues, connection/channel capacity,
inactivity and two-phase signal drain. Metrics/configured log formats are present.
The original 35 Haskell baseline is qualified; the candidate must pass those same
unchanged assertions and protected supplements, then switchback and combined
three-Go validation before support is claimed. Unit/race tests already compare
ten actual Haskell HTTP captures and check classic/raw delivery, atomic bulk input
validation, integer precision, strict ack types/range and trusted-certificate
rejection. Unit passes do not substitute for original e2e or switchback.

Use `python3 go/check.py cannon` with the pinned compiler on PATH for the full
format/tidy/verify/vet/race/build/vulnerability/license checks. The module is
AGPL-3.0-or-later; dependency proof and pinned versions are in the protected oracle.
Serious inherited concerns (payload logs, explicit TLS bypass, invalid drain
configuration, expiration units) remain recorded in the contract, without
bundling protocol/storage/security changes into the language replacement.
