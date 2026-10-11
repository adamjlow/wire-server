# Federation profiles and reciprocal connections: partial qualification

Tested source `b653fe615974533002e471d4ef5407c56fdbbdcb`, Brig SHA256
`91fb27f6d85354510b173160fd48b7492867484142ec6a36adaec188b1298284`.
Pinned Go1.26.9 format/dependency/vet/unit/race/security/license/build checks,
static original-runner unit execution and tagged store compilation pass.
SDK identity and exact artifacts are in `aggregate-sdk-result.json`.

Five actual native/Go federation get-users-by-ids responses match status,
media and complete JSON: empty, regular, identityless, missing and ordered
duplicate IDs. Public profiles exclude private self-account fields; projection
uses configured email visibility, native legalhold and app injection, bounded8
ordered reads, pending omission and strict expired-before-now deletion enqueue.
Units verify privacy/visibility, pending omission, equal-versus-past expiry and
typed remote admission. No private data or password enters the public projection.
The outgoing connection decoder now validates the complete native profile shape
rather than reading only type/team; legacy picture/assets fallback is retained.
Finite tests do not establish exhaustive parser identity.

Go creates a remote connection and preserves native deterministic conversation
identity. Actual native B then reconnects after rescind, Go A becomes pending,
Go A accepts and native B becomes accepted. Native A restarts on the unchanged
stores and reads exactly the same Go remote connection. Local transitions,
nine delivered Gundeck notifications and opaque cursor readback also pass.
Evidence is `aggregate-paired-reads.json`; no connection rows were injected.

**The complete run fails.** In both attempts,21 of22 unchanged original tests
pass:14 MLS and seven Connection variants. The remote-owned conversation test
can need to delete a generated user and retry until ownership matches. Go
DELETE /v19/self returns405. Account deletion is a genuine required missing
contract; the original fixture/assertion remains unchanged. Both XML/log/result
sets retain the failure. The observer accumulates original failures and still
fails at completion, allowing independent controls to finish without disguising
a suite pass.

The aggregate run additionally fails client switchback: its new profile control
looked up the existing short-lived identityless lifecycle user after expiry,
correctly enqueued deletion, and native restart consumed it, removing the newly
created client. This is interference between observer actors, not evidence of
successful client switchback or of a candidate storage fix. The subsequent
observer uses a separate identityless profile actor with expires_in3600.
That observer correction has only Python syntax/scope checks; it has not yet
been run against the backend. Earlier independent client/signed-state proofs
retain their historical source scope, not a pass for this failed run.

Both projects have25 managed exits0, runner1, cleanup0 and independent empty
container/network/volume audits. SDK namespaces are independently empty.
Initial source0bc2d9de9 SDK failure came from an invalid synthetic empty redundant
profile UUID; corrected candidate fixture only. A later preflight lacked disk
space before starting any backend. Retained SDK files were relocated with hashes,
owners/groups/modes verified and original-path symlinks preserved; no evidence
was discarded. Receipts record both conditions.

Same-author scoped review under the user's explicit self-review delegation:
accept the demonstrated profile/reciprocal increment; reject any claim that the
original22 or whole535 acceptance passed. Team/app/LH variants, full team-member
peer decoding, account deletion, remaining public profiles/writes and broader
Brig families remain unqualified. Whole final eight-Go, three user modes,
all-entity switchback, operations/image/default selector and legal/security
acceptance are still incomplete. No gates advanced, no new push. Haskell remains
supported/default. Serious inherited security concerns remain in the existing
service notes; this increment changes no security policy.
