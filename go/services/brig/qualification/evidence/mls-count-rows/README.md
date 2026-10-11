# Public MLS count and durable cleanup comparison

Qualified source `94701681658612111a7a2a3171f1778c1203bd58`; static Brig
SHA256 `e85ddd5c418342be39b2462f8fe889ab69ddf4c66fa0cf2a4f909dfb0dcb60e5`.
Full pinned Go format/dependency/vet/unit/race/security/license/build checks and
static normal tests pass. Tagged store helper SHA256
`a9101520b97c1a23dbcb3f0e6d5c8fcf950cce3e050c97118308dadef464e5a3` was
actually executed six times in the identified nonroot isolated runner.

Public count preserves native version cutoffs, optional/default suite until V8,
required query from V8, user parsing, MLS feature guard and supported suites.
Five native/Go controls pass: V4 removed, V7 default, V19 explicit suite,
unknown suite and unknown client. Ten existing MLS client controls now exercise
an actual usable package and MLS=true. Prior14 public clients/13 federation/
5 single/5 multi/7 lifecycle/17 paired controls pass as well.

The supplemental fixture writer uses parameterized CQL and never invokes the
candidate decoder. It stages five synthetic byte strings: usable, expired,
future-dated, non-key-package leaf source, and malformed. Native count returns1
and deletes exactly the three decoded unusable rows. Go reads retained native
bytes, receives the same five rows, returns1 and preserves the same two rows.
After switching back, native returns1 and reads identical Go-retained bytes.
All six raw snapshots and exact status/media/JSON comparisons are in
[paired-reads.json](paired-reads.json). This demonstrates actual database
cleanup and persistence, not upload-time cryptographic validity.

Runner0, cleanup0, all25 managed processes exit0, no force; independent audit
finds no owned container/network/volume. The first attempt failed before any
backend startup because its helper mount was outside the isolation allowlist.
The fix uses the existing read-only generated fixture mount and verifies copied
helper identity. Protected runner, assertions and native sources remain unchanged.

Same-author scoped review, under the user's explicit self-review authority,
accepts these results for this slice only. Native count intentionally retains
malformed blobs and performs syntax/lifetime checks without signature or
credential validation; preserve this inherited behavior. Upload/claim/replace/
delete still require their native validation contracts and original tests.
No original compiled suite was executed here. Whole Brig, final535/eight-Go,
all-entity three-mode switchback, operations/image/default-selector and whole
review remain pending. Haskell stays default; no service gate is advanced.
