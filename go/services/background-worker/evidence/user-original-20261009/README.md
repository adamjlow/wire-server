Original user tests against connected Go worker: 1/2 pass, not qualification

Exact clean Go1.26.9 linux/amd64 CGO-disabled source568810e637d656f08d54789b5d9408e83c5c53bc
binary3740244e3a4a4c06c876948d50d62d05a18fe98ac71f7f0674d0d2c6318a1145
ran unchanged background-worker-users assertions in the existing owned Docker
harness, with every other service Haskell. Runner image and all hashes are in
build.json/environment.json/suite.json; original.xml is the unmodified report.
All test data and credentials in the original failure context are synthetic,
created inside disposable private dependencies. No shared/production targets.

Strict XML has two distinct executions: testReindexingUsersDuringMigration passes,
testUserMigrationToPostgres fails (SCIM GET HTTP500 expected200). No errors/skips.
The failing source call stack identifies runPhase at User.hs:138, which is
runPhase2; its overrides explicitly set migrateUsers=false. The failure occurs
in checkUnaffectedUsers before phase3 enables the migration. The reindex test
also uses phases1/2/5 only and never enables migrateUsers. Consequently this run
does not execute or validate the new candidate user DB migration. The pre-enable
SCIM/read failure is unresolved; prior native references also retained user-test
failures. Do not waive it or attribute a successful migration to the passing case.

Runner process1/controller2 (failure), owned cleanup0. Post-cleanup project label
queries for containers, networks and volumes were all empty. Project identity:
wire-e2e-426186ffb4db49498e8ff2e50e2e1a7e. Raw controller/baseline/dependency logs
remain in the external cache user-runtime-original; terminal, no live process.

Complete remaining conversation/scheduled behavior and resolve the native
pre-enable failure before final all47/all124 qualification. Original whole
profile also contains handle-dispute/invalid-user cases that enable migration;
those remain required, with unchanged assertions. Actual new DB effects and
final switchback/operations/image/selector/review remain unproved. Goal/task
active; no compatibility or availability claim.
