# Scoped federation prekey proof

Production source `8ada940ca51925b2dba20fb284c39f4b88298117`,
observer source `74d4411f58e8a5c3bf650cd48441d2a6b8f443be`, exact static
service SHA256 `1aa9256bdddc6fed1ce63bec2457ffb9d830be2aece6a81d7b003ae61929d7ed`.
Full pinned Go formatting/dependency/vet/unit/race/security/license/build checks
and static normal units pass. Observer-only rebuild independently verifies
identical production/normal/tagged-store binaries. Tagged stores compile only.
No new Go module dependency is required for standard-library h2c.

Actual two-backend native reference and Go Brig/Gundeck/native remainder
match nine federation responses exactly by status, content type and parsed JSON:
outgoing single/bundle/missing, mixed local/remote multi V3/V19, incoming
single/null/bundle/multi. Native backend B owns a last-resort-only client so
comparison uses identical durable state without token/time normalization.
Prior five local single/bundle controls, five multi controls, seven lifecycle
controls and unchanged17 paired controls also pass. Actual Cassandra native-Go-
native client creation/deletion/ordinary-prekey/retained-MLS switchback passes.
All25 managed exits0; runner0/cleanup0; independent project label resource audit
empty. No forced stops and no new original compiled suite execution claimed.

Focused tests use a real stdlib h2c peer: protocol2/origin/request-id/header/body,
per-call highest shared version with legacy0, numeric version validation,
remote prekey identity/text acceptance, error data/inner forwarding, V3 error
versus V4 success-plus-qualified-failed-users and incoming admission/null behavior.

This is scoped proof, not whole Brig/current eight-Go535 qualification.
Native transport timeout review found TCP30s versus initial candidate10s;
subsequent correction will have a distinct source/binary and checks. Exact
malformed-body/transport error text, response limits/deadlines, alias maps, live
failure/partial and bounded remote concurrency controls, actual Dynamo claims
and other whole families remain pending. Preserve these gaps rather than
advertising this subset as a completed service.

Inherited concerns: native trusts remote identity/key text without cryptographic
verification; federation prekeys use a real Galley call with
LegalholdPlusFederationNotImplemented, whose native policy skips consent.
This implementation preserves them; remediation is separate scope.
User-delegated same-author review accepts only the stated implementation and
proof, not independent review or final whole approval. Protected Haskell/oracle/
fixtures/assertions/defaults/security policy unchanged. Registry remains
implementing, compatibility/review null.
