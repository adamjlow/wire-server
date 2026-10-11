# MLS claim and fourteen unchanged compiled tests

Qualified source `f4d5a5343dc4bcbe80fd447031679d052ca74316`, static Brig
SHA256 `dd3a93f56f694ce5481cc6f3e08aaadf203df23425b4524ffa4ea2cee559b665`.
Full pinned Go format/dependency/vet/unit/race/security/license/build checks and
static normal tests pass. No dependency or native/oracle change.

Claim preserves account activation/status admission, target legalhold lookup
before full client lookup, local caller consent guard, self-client skip only
for the same qualified user, process-wide cancellable claim gate, native expiry
cleanup and random usable package selection followed by ref-equality deletion.
Empty clients/packages return an empty bundle; partial earlier consumption is
not rolled back. Incoming federation returns null for disabled MLS and ordinary
typed admission failures. Outgoing federation validates raw package signatures,
identity, lifetime and reference, and stored MLS keys for local bundle identities.
Qualified/numeric/blob Set ordering and duplicate elimination are implemented.
Basic History credentials now parse and fail identity equality as native does.

Fourteen unchanged compiled Haskell cases pass with dynamic Go Brig, Go Gundeck
and remaining native peers: prior seven upload/count-four-suites/delete/unsupported/
no-key tests, plus ephemeral/local/self/remote/multi-suite claims and replace/V7.
These exercise real mls-test-cli signed packages, reference hashes, single
consumption, skip-own behavior and replacement count/ref effects, including
empty replacement and invalid announced-suite admission. Strict expected XML14
contains no failure/error/skip; XML SHA256
`c1b282be1319b73259119f31cc4b4ae86dcde4d547c53209c275ae93751e6cac`.
Original integration SHA256
`56f56a8e0f8ffd24a11982f13385d41f4d92e238e6900bcbca1ddf7d21825f4a`.
Prior four deletion/five count/ten MLS/14 public clients/13 federation/five
single/five multi/seven lifecycle/17 paired controls and actual client/prekey/
MLS retained-byte native-Go-native switchback pass. All25 managed processes
exit0, runner0, cleanup0, no force, independent exact owned namespace audit empty.
Runner queue credential teardown notes retained, not claimed as external cleanup.

Same-author scoped review under the user's explicit delegation accepts stated
evidence only. Two original MLS account-deleted/suspended cases require unfinished
team invitation/account-deletion APIs and remain mandatory, not waived. Actual
signed-package upload/claim/replace native-Go-native state proof, native destructive
replacement failure, legalhold backend edge/status witnesses, X509/DER/identity/
Base64/malformed diagnostics, remote malformed-bundle/error-priority variants
and version cutoffs remain to qualify before whole acceptance. Remote set
normalization occurs after per-entry cryptographic validation; compare native
sorted-set validation priority and align before final acceptance.

Inherited concerns: native X509 credential handling lacks certificate-chain
signature/trust validation. Claim locking is process-local and CQL deletion is
unconditional, so cross-replica duplicate selection is a source-based concern;
no multi-replica exploit was demonstrated. Preserve native behavior rather than
silently changing security/transaction policy. Whole Brig/full535/eight-Go/
all-entity3mode/operations/image/default-selector/legal/security/whole review
remain incomplete. Haskell stays supported/default, no service gate advanced,
and these commits are local only.
