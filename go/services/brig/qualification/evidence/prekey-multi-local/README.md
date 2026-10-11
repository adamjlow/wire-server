# Local multi-user prekey API/state qualification

Source `7911900d2183263ba353e716c702a052be6bdc02`, service SHA256
`ea11e4ac6fd36f61388a686a7465a507866910a3c40a1d044be048a9ecdda1c1`.
Full pinned Go1.26.9 checks/static normal units pass; tagged stores compile only.
No new original compiled suite/native Haskell build; whole Brig incomplete.
Retained prior candidate failure d5a2d11df used settings instead of optSettings
in its new test fixture. Candidate fixture key corrected, assertions unchanged;
required max conversation size also validated before any listener/dependency
initialization. Native maxConvSize Word16 configuration now binds API user limit.

Source contracts: public Brig PrekeyAPI/getMultiUserPrekeyBundle handlers;
Wire.ClientSubsystem.Interpreter.claimLocalMultiPrekeyBundles;
Wire.API.User.Client.UserClients/QualifiedUserClientPrekeyMapV4.
Legacy unqualified POST/users/prekeys is UntilV2; qualified POST/users/list-prekeys
returns raw domain map beforeV4 and qualified_user_client_prekeys wrapper atV4+,
without failed_to_list when no failures. Request normalization preserves first
identical JSON property, validates domains/userUUID/client-string arrays, sorts/
deduplicates numeric ClientIds and counts unique users per domain before effects.
Over-limit returns native403/too-many-clients; bound includes remote domains even
though remote execution is still pending. Native empty qualified map has no local
group/guard, whereas legacy empty map invokes guard. No body normalization waiver.

Local group guard occurs once before claims. Users are UUID ordered in sequential
chunks16; up to16 users claim concurrently in each chunk, client claims per user
remain sequential/numeric. Missing prekeys are explicit null and invoke native
claimOrClean lookup/deletion with connectionNothing. Failure cancels/join workers
without rollback of consumed keys or other committed effects. Focused finite
units deterministically synchronize17 users: first16 reach claim before release,
17th cannot enter until first chunk joins, peak16; order,17 null results and17
cleanup calls verified. Strict decode/duplicate/limit/version/Word16 tests pass.

Unchanged frozen random-mode fixture actual native/Go five multi controls match
status/media/JSON: legacyV1, qualifiedV3, qualifiedV19, empty qualifiedV19 and
17-user over configured16 limit. Requests include duplicate client ID and unknown
clientf, exercising deduplication and null output on real Cassandra/Galley paths.
Five single/bundle/last/missing controls, seven lifecycle admission controls and
original17 paired controls also pass; actual client creation/deletion/ordinary-key/
retainedMLS native-Go-native switchback remains passing on identical stores.
All13 managed exits0/runner0/cleanup0; independent project audit empty/no force.
This is direct two-Go Brig/Gundeck/native-remainder API/state evidence, not final
eight-Go/whole535/all-entity three-mode qualification. No new original suite claim.

Remote outgoing federation and incoming prekey RPCs remain unfinished. V3 fails
on remote federation error; V4 keeps successes and lists qualified affected users.
Native remote pool limit8 is distinct from local16. Pinned Go stdlib supports
unencryptedHTTP2 for native federator internal transport; no new dependency is
needed solely for h2c. Next implement version negotiation, original request-id/
origin/version headers, typed payloads/errors, both failure variants, remote
single/bundle/multi and actual native-peer qualification. Live Dynamo, concurrent
real claims, corrupted no-prekey repair, real legalhold403, SDK/Read Word32/parser
corners, logging/metrics and all other whole families remain pending.

User-delegated same-author scoped review accepts stated implementation/proof only;
not independent or whole approval. Haskell/oracle/fixture/assertion/default/
security policy unchanged. Preserve native nontransactional effects and inherited
best-effort lock concerns. Available/compatibility/review gates stay unset.
