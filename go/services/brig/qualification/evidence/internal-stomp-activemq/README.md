# Actual ActiveMQ replay and redelivery comparison

At source `574040ddebe2cb1a73dbbf9214a0437bc03d0b3b`, service
`42ea8a7641445628d976dea7480fec13885f60f82d0344567a886b79d2b3da5d`,
[pinned Go checks](sdk/go-check.log) and [static runner units](sdk/runtime-units.log)
pass. [Receipt](sdk/build-receipt.json) records identical service/unit/store bytes
to the preceding STOMP implementation. Observer/fixture changes did not change
the executable. The unchanged compiler's native-library license/archive evidence
is retained in [the preceding qualification](../internal-stomp/sdk/).

The broker uses Apache's [documented Docker distribution](https://activemq.apache.org/components/classic/documentation/docker-image), pinned ActiveMQ6.3.2 amd64 manifest
`sha256:1cacfbd9c08ad437c5e1177a9f1217e7c7ada62a64065128acd1a81866a03711`,
image `sha256:b9972c335fae8deb648f3f048622aaf3540c82d5c42741c9e6dad43af1e18180`.
[Registry manifest](activemq-6.3.2-manifest.json) and
[actual broker log](paired/activemq.log) retain its identity/startup.
The fixture enables authenticated STOMP explicitly; the upstream image disables
that connector by default. It runs nonroot, read-only, with dropped capabilities,
bounded resources, private Docker networking and disposable tmpfs data.
There are no host ports. Synthetic JAAS credentials protect both STOMP and
POST-only read-only Jolokia statistics. Management binding/IP admission is widened
only within this owned namespace; no production configuration changes.

[Paired observations](paired/paired-reads.json) prove:

- Haskell consumes and acknowledges a queued account/property deletion.
- After Haskell stops and its listener closes, persistent work for another
  Haskell-created account/property is enqueued. Go consumes it, writes the native
  tombstone, removes properties and acknowledges it.
- Restarted Haskell reads that same tombstone and empty property set.
  QueueSize/InFlightCount are0 in all three observations.
- Haskell and Go each reject the same malformed event without acknowledgement:
  broker DispatchCount rises by at least2 while DequeueCount remains unchanged
  and QueueSize stays1. The broker redelivers the event. A fixture consumer
  acknowledges it only after the service owner stops; cleanup counters return0.

Native counters change from Dispatch1/Dequeue1 to Dispatch3/Dequeue1 for its
negative control. Go counters change from Dispatch35/Dequeue4 to Dispatch37/
Dequeue4. Different cumulative counts are expected because the native shutdown
interval allows additional redeliveries before ownership ends; exact timing or
dispatch-count identity is not a compatibility claim.

All [22 unchanged original executions](paired/mls-upload-original.xml) pass,
with no failures/errors/skips: [log](paired/mls-upload-original.log),
[backend receipt](paired/result.json). Existing client/prekey/raw+signed MLS
durable switchback and paired admission/effect controls pass. This is Go Brig/
Gundeck and Haskell remainder, plaintext authenticated STOMP with random prekeys.
All25 managed processes exited0, runner/cleanup0; the
[independent audit](cleanup.json) found no remaining owned resources.

Four unsuccessful broker setup attempts are retained beside the successful
[fixture smoke](stomp-broker-smoke-jolokia/result.json): direct-JAR command,
Spring property resolution, management readiness and Jolokia policy location
were corrected in the disposable fixture. Each cleanup was independently empty.
The [preflight failure](preflight-failure.json) started no backend and did not
lower the disk floor. [Historical fixture archives](stomp-historical-fixture-maintenance.json)
retain original SHA-verified bytes/mode/ownership; [identical-artifact deduplication](sdk/stomp-identical-artifact-dedup.json)
retains verified original executable paths, bytes, mode and ownership.

The final observer edit restores explicit required counter keys on the SQS
branch; STOMP snapshots were explicitly constructed with both required keys and
the passing observations contain them. No original assertion was modified.
Same-author review under the user's delegation accepts this increment only.
Trusted positive TLS, optimistic-Dynamo/STOMP configuration and strict native
YAML scalar types still need coverage. Provider/service deletion and final whole
Brig535/eight-Go/operations/all-entity/image/selector gates remain incomplete.
Haskell sources, defaults and service stages are unchanged.
