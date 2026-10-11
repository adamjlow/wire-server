# Original client registration capability tests

Source `1926a1c28f2ec7a5bd660f2225b47781c8e75449` qualifies the unchanged
production executable `fd9052f985ceec69d3260bceb94130eb75a84509e77ee13f303a1dc205ac9eac`.
A guarded observer-only rebuild reproduced both service and normal-unit bytes
from the full passing Go checks at `a3980be43743ab031e9f2f62a44902b845d7d654`;
static normal units passed in the unchanged original runner. Tagged stores
compiled only. No production, reference, oracle or fixture changes in this run.

All three unchanged compiled original Haskell tests passed, with the original
strict XML checker requiring exact names/counts and no failures/errors/skips:
Client.testCreateClientWithCapabilities,
Client.testUpdateClientWithConsumableNotificationsCapability,
Client.testGetClientCapabilitiesV7. Original XML SHA256
`9cd679807addb7615865525304508e5043b176ae9486c228ab2806f30f2783ea`.
Actual registration, CQL state, Galley/Gundeck interactions and v6/v7/current
capability responses were exercised by their original assertions.

The existing 17 finite paired native/Go controls also pass. Backend A uses Go
Brig and the identified Go Gundeck; backend B and other peers are Haskell, with
native Nginz and Go Brig for original dynamic namespaces. This is a scoped
two-Go run, not the final eight-Go mixed deployment. All25 managed processes
exit0; original process0, runner0 and cleanup0. Independent project-label
container/network/volume audit is empty. No forced process stop.

No original last-active/offline-client test, registration/deletion durable
switchback, new-device mail delivery, enabled consumable queue comparison,
internal legalhold or full Brig qualification is claimed. Existing native
failures remain retained. Brig remains unavailable/incomplete and Galley
unstarted. Same-author scoped review applies under the user's explicit
self-approval delegation; this is not independent or whole-service approval.
