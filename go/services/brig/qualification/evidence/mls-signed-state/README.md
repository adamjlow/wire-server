# Signed MLS native-Go-native state and remote Set priority

Qualified source `dd8edd104f0cc9fc507c85d8acd6db1e7b1cb484`, static Brig
SHA256 `cf3a62ac650f17ad15b05a80250dd0815ead1cbff696963f286393df758c070f`.
Full pinned Go format/dependency/vet/unit/race/security/license/build and static
normal tests pass. Tagged helper SHA256
`1a84e2f842aa0399067e982b0ebad4a8735fd40c69eac077752e06b783bc96fe`.
Original integration SHA256
`56f56a8e0f8ffd24a11982f13385d41f4d92e238e6900bcbca1ddf7d21825f4a`.

Remote claimed bundles now parse/normalize/deduplicate/sort the native typed
Set before cryptographic validation. Actual h2c regression returns a reversed
client order with two duplicate malformed lower-client entries; lower-client
TLS decoding error409 wins over higher-client invalid-signature failure, as
native sorted validation requires. No comparison normalization is relaxed.

The tagged signer uses the independent test signer, emits only public signed
material and never calls candidate validation or writes signed rows. A separate
native actor/client/key is created using actual APIs. Native uploads a valid
synthetic signed package; CQL readback records exact bytes and computed reference.
A cryptographically signed protocol2 replacement is rejected after native has
already deleted prior rows, then native restores through upload API. Go reads
identical state, claims the exact native package/ref and removes it. Go uploads,
returns the same destructive replace error and empty rows, then successfully
replaces through API. Native restarts on the same stores, reads identical Go
bytes/ref, counts1 and claims/removes that exact signed Go replacement. No direct
signed-row injection or fixture reset substitutes for these transitions.
All nine signed-state snapshots and HTTP outcomes are in paired-reads.json.

All14 unchanged compiled original MLS cases pass again: upload/count-four-suites/
no-key/unsupported/delete/local/self/remote/ephemeral/multi-suite claim/replace/V7.
Strict XML14 has expected names and no failures/errors/skips, SHA256
`62a375941d0eb463122810e0e8131cd66465ca6e10fc57d95a5ee3922b212ec8`.
Prior raw expiry-cleanup/client/prekey/MLS comparisons and switchbacks also pass.
All25 managed exits0/runner0/cleanup0/no force; independent project audit empty.
No new module/host dependency, native/oracle edits or gate advancement.

Same-author scoped review under explicit user delegation accepts these results,
not whole Brig approval. The signed state proof uses the configured native user
storage mode; it does not establish all three user modes. Two original team/
account MLS cases, legalhold/backend/version/error/X509/DER/Base64 variants and
other mandatory Brig families/full535/eight-Go/all-entity3mode/operations/image/
default-selector/legal/security/whole review remain unqualified. Native X509
chain trust omission, process-local claim lock with possible cross-replica
selection duplication, and destructive replacement ordering remain inherited
behavior/concerns, not repaired. Haskell supported/default; commits local only.
