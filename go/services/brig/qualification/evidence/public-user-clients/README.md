# Scoped public user-client read proof

Source `6351c13e9b375f60017ae26ade2449b8bbf60964`, static service SHA256
`dce9b0a9735b0c9b3a8dee3149cd5d96a7e1e847045afdc142e6f07f0e9e3a66`,
normal-unit SHA256 `7d26e332841cf25a2dfe78e4a7ab2b8b8e9b3bf6f05b35d56c0b88c843a9cb13`.
Full pinned Go format/dependency/vet/unit/race/security/license/build and static
normal units pass. Tagged stores compile only. No new module dependency.

Real CQL public reads query only client/class with LocalQuorum/retry1, matching
native source. They do not consult orphan/corrupt MLS/private metadata. Results
are numeric-ID/class-ordered sets with optional class. Local bulk pool50 preserves
input-index map overwrite behavior. Remote domains are sequential before local
reads, with failures logged/omitted; local domain is always present. Gateway
header requirements and native public projection/version boundaries are retained.

Exact native/Go14 status/media/parsed-JSON controls pass: unqualifiedV1/qualified
V15 list, removed qualifiedV16 list404, qualified single/missing, remote list/
single, inaccessible remote list empty/single404, bare legacy bulk, legacy V2
wrapped bulk, V19 wrapped bulk with healthy/failed remote domains, incoming
federation get-user-clients and empty users. Owned supplemental requests use
native-created last-resort-only clients on identical frozen initialized stores,
without broad normalization. Focused units validate class/ID projection, alias
set deduplication, remote-first/failed omission, unconditional empty-local map,
version cutoffs, bulk wrapping/authentication and invalid admission before reads.

Prior13 federation/5 single/5 multi/7 lifecycle/unchanged17 paired controls and
actual client/prekey/MLS native-Go-native creation/deletion/key/retention
switchback also pass on this binary. Two Go peers (Brig/Gundeck), native backendB
and remainder; all25 managed exits0, runner0, cleanup0 and independent namespace
audit empty. No forced stop and no new original compiled suite claim.

Native public GET does not request Z-User; bulk validates but otherwise ignores
it, trusting the gateway. Preserve that boundary rather than adding auth policy.
Inherited federation/prekey security concerns from prior evidence remain.
Public parser-error detail/aliases/corrupt-store qualification, local50 bound
proof, MLS/client key-package reads and other whole families remain pending.
Old prekey malformed-body/transport messages, response caps/deadlines and actual
Dynamo/no-key/legalhold controls are still explicit gaps. No whole535/eight-Go/
all-entity3mode/loaded-operations/image/default-selector/available claim.

User-delegated same-author review accepts the stated implementation/proof only;
not independent or whole approval. Haskell/oracle/fixtures/assertions/schemas/
defaults/security policy unchanged; implementing/compatibility-null/review-null
service gates retained. Subsequent work remains local, no push or deployment.
