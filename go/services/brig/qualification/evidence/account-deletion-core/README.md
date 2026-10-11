# Account deletion engine and durable cleanup adapters

Production/normal-test source `dae7c8987c819ffafde7b766f2e1037fe1e52a4c`,
SDK Brig SHA256 `f66e431bddb27eca312cce9ff9d6ebf766f900b2ce2ec4de21e2dbf5907355ee`.
Pinned Go1.26.9 formatting/dependencies/vet/unit/race/build/vulnerability/license
checks and static original-runner units pass. Native C-library/license inputs
are unchanged; SDK image and archive manifest are pinned in the receipts.

The engine preserves native self-deletion admission: missing account invalid;
already deleted public account returns immediately; active/suspended team owner
guard precedes authentication; supplied password uses the password-only lookup
and native verifier without upgrading the hash or ordinary reauthentication's
status/SAML exceptions; no-password verified email uses the verification-code
branch; no verified identity deletes directly. Explicit mandatory effects fail
before partial cleanup if a dependency is absent. Interrupted phases stop further
effects. Internal queue replay deliberately finishes cleanup even for tombstones.

Cleanup order is email key/properties, user tombstone, team invitation/SCIM/group
cleanup, peers/assets, client/prekey storage, search index, deletion events,
connections, cookies. Actual callbacks for peers/groups/events/code delivery and
HTTP/queue binding remain unfinished. **This is an unbound engine, not a working
DELETE/self endpoint or recovery of the failed original22 suite.** No API/e2e
or whole-service qualification is claimed by this increment.

Storage follows native source: Cassandra frees only an owned handle/claim,
subtracts this UUID from the claims set using TTL3600, then writes the precise
native tombstone retaining unrelated columns. PostgreSQL performs serializable
DELETE RETURNING created_at and inserts deleted_user only for an existing account,
preserving team/creation timestamp and existing tombstones. Migration mode uses
the native shared migration lock and per-user store selection. Team invitation
cleanup reuses the existing adapter, then removes the native SCIM pending row.
Connection cleanup reads native pages100, deletes reverse rows with concurrency16
and native consistency/retry settings, then clears the local/remote partitions.
No request-controlled table name is interpolated into a query.

Actual native-schema store qualification passes at source
`66474b799d55dd77f63601735553880d399a0842`, tagged binary SHA256
`f10639b126521adf049262eccc0c745def4d613368b63527bdd9362f40c16424`.
The only changed inputs since the full SDK checks are the tagged store fixture
and plan, verified by exact Git path comparison. Only that fixture was rebuilt;
production/normal-test/dependency checks are reused with their honest prior
revision. All three selectors' tombstone shapes, repeated/missing PG deletion,
Cassandra-resident migration users, reassigned email/handle ownership, other
unique claim preservation and101-contact multi-page cleanup pass. A real locked
concurrent PG update forces the serializable deletion to retry and produce exactly
one deletion record. This verifies actual isolation rather than a mock error.
The native schema utility and pinned runner/database images are unchanged.

Retained first failure: the fixture expected an unvalidated email to survive,
but native-shaped CreateUser never writes that column. The corrected fixture
explicitly stages and verifies the later email-change state before deletion;
the retention assertion is unchanged. Code review also fixed generic error
wrapping inside the PG transaction, which would hide40001 from its retry loop.
Both full SDK receipts, failed and passing store reports, stdout/stderr and
native-schema output are retained. All controllers are terminal and independent
container/network/volume audits are empty. Limited owned stale compiler-cache
maintenance is recorded; no retained artifacts were removed.

Inherited concerns, preserved for parity: Cassandra's native account tombstone
retains the password hash and unvalidated email rather than erasing every private
column. Deletion is nontransactional across storage/peers; interruption after the
tombstone can leave downstream state, while a repeated public self-delete returns
immediately. Internal replay remains necessary. With no verified identity, native
no-password self deletion can proceed even when a stored password exists; this
relies on the established gateway identity boundary. Changing retention,
authentication or recovery policy is separate work, not a migration fix.

Same-author scoped review under the user's explicit delegation accepts the
engine/storage progress and bounded evidence, not complete account deletion or
Brig. Next bind native team/group/peer/notification/journal/code/queue paths and
public/internal/federation APIs, then rerun the unchanged connection22/affected
account/MLS originals with separate profile/lifecycle actors. Whole535/eight-Go,
all-entity native-Go-native across three modes, operations/image/default selector
and legal/security acceptance remain incomplete. No gates advanced or push.
