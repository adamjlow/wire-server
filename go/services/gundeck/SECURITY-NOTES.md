# Security and compatibility notes

Mirror the source API and current persisted schemas. These observations do not
change Haskell defaults or approve a production switch.

- Haskell lifecycle/debug logs can expose token values and whole notification
  payloads. Go logs generic operational errors and safe categories, without tokens,
  credentials, payload contents or customer identifiers.
- The internal service APIs trust their network boundary and principal headers.
  Presence resource URLs can cause outbound requests. Preserve that contract;
  deployment network restrictions remain essential. A separately reviewed
  authentication/URL policy would be a future backend change.
- Haskell permits optional unbounded native concurrency. Go uses bounded workers
  (32 when unset), honors configured native concurrency and the same shared SNS
  budget admission rule. This changes scheduling, not recipients or API responses.
  The source budget checks previously allocated cost before admission, so its
  documented hard bound can overshoot for multi-token actions; Go preserves that
  admission behavior rather than changing notification delivery decisions.
- Native SNS message attributes are empty in the actual source call. Notification
  TTL is enforced in Cassandra and RabbitMQ, not added as an SNS message attribute.
- TLS chain and hostname verification remain enabled. Haskell's TLS bypass flags
  are unsupported by Go; retain Haskell if an existing deployment depends on them.
- Haskell reference shutdown intermittently exits 1 with an MVar deadlock. Retain
  that reference failure; Go must pass clean bounded idle/in-flight shutdown.
- JSON validation rejects malformed UTF-8, trailing values, invalid UUIDs and
  client IDs outside Word64. Cassandra queries are parameterized. Client IDs use
  the source's canonical lowercase representation without leading zeros.

Pinned dependencies and module vulnerability/license checks are retained in the
service evidence. Assembly inspection warnings for xxhash/x/sys are limitations
of the license scanner, not waived findings or a claim that assembly was audited.
