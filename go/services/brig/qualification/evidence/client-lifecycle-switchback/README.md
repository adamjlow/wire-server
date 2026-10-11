# Actual client creation/deletion switchback

Source `27b31839939758ba45d8c51ab65417e092a78297`, executable
`d109530d48f490ccc6b1eaf159426ecfaf715dc786116b363cf59a19fdbc7a5b`.
Full pinned Go1.26.9 checks and static normal units in the unchanged original
runner pass; tagged stores compile only. Native reference/runner/peer/binary
identities are retained in the receipts. Existing static linker warnings remain.
No new native Haskell build or original compiled integration suite execution.

On the same initialized real Cassandra stores: native creates a password-bearing
identityless actor and permanent client with ordinary/last prekeys and MLS key;
Go reads the native state unchanged, creates a distinct permanent client through
public POST with password reauthentication, then deletes the native client via
public DELETE. Native restart reads the exact Go-created response/state including
creation timestamp, reads identical deleted-client404/empty prekey list, claims
Go-written ordinary prekey7, then recreates the deleted client without new MLS
keys and reads the retained original MLS key. Dynamic positive creation times are
not normalized or compared between independent creations: the same persisted
Go-created row is read by native. Real configured queue/Dynamo deletion paths
are exercised; queue-consumer delivery/fault behavior is not separately asserted.

Seven exact native/Go status/media/body admission controls pass: manual LH before
missing-user lookup, missing user, second permanent client missing/wrong password,
permanent deletion missing/wrong password, and unknown-client deletion. Existing17
paired controls remain unchanged/passing. Failed prior sourcee39a1005e is retained
in ../client-lifecycle-missing-media: storage deletion worked but missing-client
GET404 media differed. Candidate now returns native text/plain;charset=utf-8;
assertions were not loosened or native source repaired.

All13 managed process exits0, runner0/cleanup0 and independent project audit empty;
no forced stops. Backend A has Go Brig/Gundeck and native remainder, no backend B
or Nginz; this is a finite direct-API switchback, not final eight-Go/whole535 proof.
The earlier four original client cases remain sourcec2d03121a evidence, not a
fresh run claimed for this corrected executable. Actor has no email; new-device
mail delivery is not exercised. Temporary replacement, cookie-label revocation,
legalhold internal lifecycle, enabled consumable queue and complete error/parser
matrix remain outside this witness. Whole Brig remains incomplete/unavailable.

Same-author scoped review under explicit delegated self-approval accepts this
source-aligned implementation and real storage witness; not independent or whole
approval. Protected reference/oracle/assertions/fixture/default/security policy
remain unchanged. Native deletion retains MLS rows and nontransactional effects.
