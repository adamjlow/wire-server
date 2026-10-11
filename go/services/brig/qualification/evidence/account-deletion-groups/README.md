# Account deletion group cleanup

Group source `954abe23c9f03a4bd2567b3b417c5822e597ae6b` passes
[full SDK/static checks](initial-sdk/build-receipt.json) and
[actual native-schema stores](stores/result.json), tagged executable
`26f660403590c397c40eeec73d79441f43333e707bf5c4aa7ab8aaa2dd2a3c94`.
Native schema tool and store executable exit0; the store assertions finish1.86s.
They verify17 target groups in15+2 pages, descending same-time UUID cursor,
PostgreSQL group storage under all three user selectors, sequential deletion of
target memberships, preservation of17 other-user and one other-team membership,
and rejection of invalid group metadata. The preexisting account/identity/client
store controls also remain green. These are candidate store tests on native
schemas, not actual native-Go-native/API/integration qualification.

Review caught case-insensitive Go struct field matching where native Aeson
requires exact names. Corrected decoder source
`627001447c8b907a8287421270bea5712f99282b`, static service
`0e1eebe9de0d6662999f6206843a58ce5176f89fca4d0eb2203d90a1181fa23c`,
passes [full pinned Go checks](member-sdk/go-check.log) and
[unchanged original-runner static units](member-sdk/runtime-units.log).
[Receipt](member-sdk/build-receipt.json) identifies toolchain/images/artifacts.
Normal tests cover member required fields, invitation pairing, legalhold enums,
permission subset/unknown bits, exact key names and integral Scientific Word64
boundary/overflow/hostile exponents; group tests cover missing member and failure
prefix, successful cursor advancement and final empty page.

Only team_member.go, its normal unit tests and the plan changed between these
sources. Store implementation and tagged fixture were unchanged, and that fixture
does not invoke the decoder. Reuse the exact initial native-store evidence;
the corrected tagged binary was compiled but not rerun on native stores.
[Cleanup](cleanup.json) records terminal controllers and independent empty owned
containers/networks/volumes. No Haskell source or protected oracle changed.

Native references: `Wire.UserGroupSubsystem.Interpreter.removeUserFromAllGroups`,
`Wire.UserGroupStore.Postgres.getUserGroups/removeUser`,
`Wire.GalleyAPIAccess.Rpc.getTeamMember`, `Wire.API.Team.Member` and
`Wire.API.Team.Permission`. Retain page15, read-only ReadCommitted transactions,
channel/team counts despite unused output, full metadata decoding, millisecond
cursor truncation, sequential deletes and native five administrative-error
retries with pool reset. Parameterized UUIDs, bounded permission conversion,
context cancellation and scrubbed diagnostic errors preserve security boundaries.
No new dependency/license surface or security policy change.

Same-author scoped review under the user's existing delegation accepts this
increment, not whole Brig. Group and member adapters remain unbound. Persistent
notifications/federation, verification/mail and internal replay must precede
public deletion and unchanged original22 retry. Whole535/eight-Go qualification,
operations and all-entity three-mode switchback remain mandatory.
