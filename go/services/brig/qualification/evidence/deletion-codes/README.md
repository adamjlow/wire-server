# Account deletion verification codes and mail

Source `41b8e552cd608aed3e2a2b49edc04b3b0b7d95c6`, service
`fbabaafb9354eb43c4e74c9db0177d4093bcbcc4f7dfd278ebcf49b53ffc2356`:
[pinned quality checks](sdk/go-check.log) and
[static original-runner units](sdk/runtime-units.log) pass.
[Receipt](sdk/build-receipt.json) identifies compiler and artifact hashes.
Earlier passing pre-hardening build is retained in initial-sdk; current source
additionally rejects a null stored throttle TTL like the native decoder.

Finite tests prove existing exhausted-code precedence, pending TTL, native
lookup/throttle/insert/mail ordering, three retries with remaining TTL unchanged,
successful verification retaining the code, and mail-failure code removal.
Native repository deletion templates render the URL, escaped HTML and exact
Delete/key/code processing headers through the existing localized renderer.
Delivery reuses the existing SMTP/SES transport, not a mock production sender.
Code values use crypto/rand15 bytes; comparison uses constant-time comparison;
record String methods redact secrets and CQL is parameterized. No dependencies
or native security policy changed. Preserve native nontransactional code/throttle
creation and its inherited concurrent-request race rather than claim atomicity.

Scope1 Cassandra methods retain native LocalQuorum and read1/write5 retry counts.
They are compiled but **not yet exercised against real stores** by this increment.
Mail unit tests prove rendering, not actual delivery. These components are not
yet bound to public DELETE/self or POST/delete. No new original integration,
whole-service, operations or switchback qualification is claimed.

[Cleanup](cleanup.json) records terminal controllers and independent empty owned
resource audits. Same-author review under the user's existing delegation accepts
this bounded increment, not whole Brig. Next: actual code/throttle native-schema
controls, account verification completion and public/internal pipeline binding;
then rerun the retained original22 failure. Whole535/eight-Go, all-entity switchback
and final operations/image/default-preserving gates remain outstanding.
