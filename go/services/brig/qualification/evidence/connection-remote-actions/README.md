# Remote connection actions and native deterministic conversation identity

Qualified source `35738642491006242d04fe0a00b21921479eed88`, Brig SHA256
`d12ea76ff877a9fd9d1ba62798abd55f15af051dee6aca7b7b376d66bcf802d4`.
Pinned Go1.26.9 format/dependency/vet/unit/race/security/license/build and static
normal tests pass. No protected source/assertion/fixture edits or gate advance.

Remote creation/updates fetch remote profiles, check remote team federation,
perform native action/reaction algebra with native version-negotiated RPC, then
Galley one2one upsert with exact qualified users/conversation/actor/membership
before row writes and notifications. Incoming action handles federation and
activation admission and connect/rescind reactions. Federation checks preserve
allowNone/allowAll/allowDynamic, static config precedence and exact per-domain
CQL lookup with LocalQuorum/retry1 and team restrictions. No allow-all fallback.
Focused tests verify remote policy rejection has no effects, peer failure has no
creation, reciprocal reaction drives accepted membership and effect ordering,
incoming policy-first rejection and blocked peer protection. Deterministic UUID
symmetry/version is tested; actual identity proof follows below.

Native APIs create activated local/remote actors. Go POST remote connection
returns201/sent; native B has pending reverse row. Both report the identical
qualified conversation UUID and owning domain, independently derived by native
and Go. Go updates blocked200, then accepted request produces sent200, then
cancelled200. Native B also records cancelled after outgoing RemoteRescind.
Native A restarts on unchanged stores and reads identical Go remote row fields/
time/conversation bytes. This is actual peer RPC/Galley/store evidence, without
injected connection rows or substituted synthetic peer responses. Prior local
updates/nine delivered notifications, cursor and signed/raw/client/prekey
switchbacks remain passing.

All14 unchanged compiled original MLS tests pass; strict expected-name XML14
without failures/errors/skips, SHA256 `f73592ea01da7adbd3effcdde4c15c659a361bd1119bc1338b377b1b04c4d47b`.
All25 managed process exits0/runner0/cleanup0/no force; independent project audit
empty. Same-author scoped review under explicit user delegation accepts this
increment, not full remote connection/Brig acceptance.

Next actual prerequisite: incoming get-users-by-ids profile RPC, including native
public projection, app injection, email visibility, pending/expiry/deletion and
legalhold fields, so native peers can initiate reciprocal public connection calls.
Current outgoing profile admission decoder reads type/team only; full native
UserProfile required-field/type/variant validation remains a concrete fidelity
gap to close with that profile work. Actual incoming action, dynamic team policy,
remote reaction/failure branches, positive MLS membership, and original connection
suites remain unqualified. Whole535/eight-Go/all-entity3mode/operations/image/
default-selector/legal/security completion remains open. Haskell supported/default;
commits remain local.
