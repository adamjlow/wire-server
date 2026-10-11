# Persistent federation notification publisher

Production source `367d55f64d928672eb9ff29000cfc5fc73d78405`, service
`439c6379e4136bfdeb3981f1defe060150668aad1455bc0be65a5a98ece6e08b`,
passes [pinned Go checks](qualified-sdk/go-check.log) and
[original-runner static units](qualified-sdk/runtime-units.log).
[Receipt](qualified-sdk/build-receipt.json) identifies Go1.26.9, compiler SDK,
static executables and original runner. New amqp091-go1.15.0 matches the approved
other-service pin; vulnerability scan finds no vulnerabilities and license scan
identifies BSD-2-Clause. Its goleak checksums are upstream test dependencies.
Same-author license review accepts the existing permitted BSD/AGPL combination;
local-module license URL warnings and native static-link warnings are retained.

Final fixture revision `f1a0acc7a4342d2134d956ea2d56673ffe51be85`, tagged
`61920cf4daa13913eef8bf6e2ac71de03f23b11000550b1678e735cc1a0b7346`:
[owned broker proof](qualified-broker/result.json) passes with the pinned native
Rabbit image and original runner. Actual consumer receives exact body,
application/json and persistent delivery mode, acknowledges, closes publisher
connection, observes a different connected session and receives the second
message. [Broker queue properties](qualified-broker/queues.json) confirm durable,
nonautodelete quorum and single-active-consumer values with native AMQP types.
This proves declaration/publication/reconnect, not broker-disk restart durability,
TLS-positive live integration or public account deletion.

The [tagged rebuild receipt](arguments-sdk/stores-build-receipt.json) records
exact differences from production qualification: candidate tagged test, Python
observer and plan only. Production code, ordinary units and dependencies remain
unchanged, so prior quality evidence is reused, not relabeled as a new full run.
No native database or original integration cases were rerun for these fixtures.

Retained failures: [initial SDK](initial-sdk/build-receipt.json) stopped on semantic
checksum ordering; [initial broker](initial-broker/result.json) reached Erlang ping
but failed before AMQP readiness; [ready broker](ready-broker/result.json) published
then rejected basic.get on a single-active quorum queue; [consume broker](consume-broker/result.json)
passed actual delivery/reconnect but controller expected an object instead of
the CLI's typed argument triples. Correct only candidate setup/observation; no
required queue/message property is waived. Final complete controller passes.
[Cleanup](cleanup.json) records terminal attempts and independently empty owned
containers/networks/volumes. Synthetic credentials only, private internal network,
no host ports, nonroot containers and task-owned tmpfs data.

Native references: Network.AMQP.Extended (settings/credentials/channel lifecycle),
Wire.BackendNotificationQueueAccess.RabbitMq (1s availability and three1s retries),
federation BackendNotifications.ensureQueue and API.sendBundle. Startup owns a
reconnecting publisher when federation is configured; shutdown cancels/waits.
TLS uses trusted roots/hostname verification and TLS1.2+, rejecting enabled
insecureSkipVerifyTls. Cancellation closes blocked AMQP transport; errors exclude
credential/peer details. Optional disabled federation returns an explicit enqueue
error, never successful no-op delivery. Native publication uses no confirms;
transport ambiguity and retries can lose or duplicate messages. Retain that
contract, document improvement separately, and do not infer guaranteed durability
merely from successful publication or delivery mode.

User-delegated same-author review accepts this publisher increment; no independent
review or whole Brig approval is claimed. Startup lifecycle is bound, but deletion
events still need the complete engine/verification/mail/replay/HTTP bindings.
The existing DELETE/self original failure remains unresolved; final535/eight-Go,
all-entity three-mode switchback, operations, image and selector gates remain.
