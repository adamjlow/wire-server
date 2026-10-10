Scheduled pool construction checkpoint, 2026-10-10

Base: 803484f312c1c77be8a2e170f301c5dd10bd92cf. Source hashes identify
construction inputs; helper SHA256 a3b20cd71532534cef5be44ff6a10101d44541ecd7ff8b82c7a571b3a4266228.
This is not a clean final worker executable or whole-service qualification.

Go1.26.9 default module checks exit0: formatting, dependency verification,
vet, unit/race, build, vulnerability and license checks. These ran before the
build-tagged Docker test was added; production/default-test inputs are unchanged.
The tagged test was separately compiled with CGO_ENABLED=0 and executed in the
owned PostgreSQL Docker fixture, exit0. All cleanup exits0, labels empty.
The first fixture failed because it UPDATEd a queue metadata row that did not
exist; it now explicitly inserts the synthetic paused queue. The failed receipt
is retained. No native schema or protected test assertion was changed.

The real native-schema fixture proves mixed malformed/valid typed claim batches
roll back attempts/ownership/leases, successful claims commit, paused startup,
owned resume and configured concurrency, registry/liveness creation, missing-row
re-registration, LISTEN/manual-cron delivery, connection termination/reconnect
and sticky readiness, cancel-marked job deletion, finite-grace exit0 with the
unfinished lease retained and no retry/DLQ, deregistration and liveness removal.
The cancel check exercises the combined heartbeat/NOTIFY path; it does not
isolate which signal won. Candidate unit tests cover notification ownership,
required types/integral job IDs, shutdown pause guards and unlimited drain.

Source trace: Arbiter b9c57eb1f8277d97616aa449bea471fe9ce14eda's
Worker/Dispatcher/NotificationListener/ChannelHandlers, Operations.claimJobsCached
and Listen reconnect logic. Native typed decoding happens inside the claim
transaction; re-registration ignores returned pause until the next heartbeat.
One dedicated connection subscribes for both queues; polling is independent,
readiness becomes sticky after the first successful subscription.

Security: fixed whitelisted queue identifiers, parameterized payload values,
strict notification JSON/UUID/types, own-worker cancellation only, bounded pool
capacity; no host ports, synthetic data, readonly nonroot/cap-drop qualifier and
owned cleanup. Native liveness location and existing inherited risks remain.

No freshly executed Haskell/Arbiter behavior comparison, concrete scheduled domain
runtime, complete config/metrics/lifecycle proof, original47/final six-Go124,
operations acceptance, actual switchback, optional image/selector or full-service
approval. Haskell unchanged; local whole Haskell build unavailable/unrun.
Same-author construction review is delegated by the user; no independent review
or whole-service self-approval is claimed. Goal/task remain active/unqualified.
