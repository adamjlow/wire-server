# Scoped federation failures and pool proof

Source `1945701fb87cf5292ccd69512150908574278c63`, static service SHA256
`1b0ddb3a7c5460c62d5d3cc5e53577ef3dcc5fb51849f3989d6b494a59949a88`,
normal-unit SHA256 `4a58d5234d45a8e46cf8f94dc76a4750cad20bf3168dc6e6a5658e65d08ff0b3`.
Full pinned Go checks/static normal units pass. Tagged stores compile only.
This source retains the timeout-aligned production executable, with new tests
and the owned supplemental observer. No dependency/reference/oracle changes.

Finite synchronized h2c test holds eight RPCs, verifies all local consumption
before federation, releases the first sorted domain as a503 failure, admits the
ninth while the others remain held, verifies no early error return, and joins
all nine. Peak8, completed9, local committed claim1; V3 failure does not roll
back local consumption. Existing V4/error-data/version/null tests also pass.

The identified Docker runner on real frozen stores/native Galley/federators and
backendB compares13 federation responses exactly: previous nine successes/nulls
plus missing-domain single/bundle/V3 failures and V19 partial results. Native
returns400 discovery-failure with federation domain/path data; Go matches status,
content type and whole parsed JSON. V19 lists exactly the affected qualified
user and preserves both successful local and backendB maps. No broad dynamic
normalization. Prior5 single/5 multi/7 lifecycle/17 paired controls and actual
Cassandra native-Go-native creation/deletion/ordinary-prekey/MLS retention pass.
All25 managed exits0, runner0, cleanup0, independent exact label audit empty.
No force/waiver and no new original compiled suite execution. This is a two-Go
Brig/Gundeck/native-remainder scoped proof, not whole eight-Go535 qualification.

Remaining fidelity/qualification: exact malformed-body/local transport error
messages, overall response deadline/body cap and parser aliases; actual Dynamo/
concurrent/corrupt-no-key/legalhold controls; public user-client/federation/MLS
reads and other service families. Keep Haskell supported/default; whole available/
compatibility/review gates unset. Native unchecked remote identities/prekey text
and cross-federation consent bypass remain inherited concerns, not repaired.
User-delegated same-author review accepts the stated scope only, not independent
review or final whole approval. Subsequent commits are local; no push authorized.
