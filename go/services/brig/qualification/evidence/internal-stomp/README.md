# STOMP queue implementation and SQS regression

Source `ee1c74c40c0e0aa839a5c52432b8be0c55280d72`, service
`42ea8a7641445628d976dea7480fec13885f60f82d0344567a886b79d2b3da5d`:
[pinned Go checks](sdk/go-check.log) and
[static units in the original Docker runner](sdk/runtime-units.log) pass.
[Build receipt](sdk/build-receipt.json) identifies the exact artifacts.

The STOMP producer uses persistent JSON SEND and a matching broker receipt,
500 ms attempts, five additional retries and 50 ms exponential backoff. The
sequential consumer acknowledges individually after callback success, with a
one-second write deadline and one-second reconnect delay. Callback/parse failure
closes the socket without acknowledgement; the broker retains redelivery control.
Cancellation closes idle sockets. The existing 60-second deletion processor is
reused. Provider/service deletion is still unavailable and remains unacknowledged.

Native configuration uses `stomHost/stomPort/stomTls`, despite example files using
`stompHost/stompPort/stompTls`. Credentials accept native YAML/JSON `user/pass`
through `setStomp`. Dynamo credentials no longer require an SQS internal queue.
TLS verification remains enabled. Credentials and broker bodies/errors are not
logged. Required fields and framing characters are checked.

The pinned go-stomp module is used for frame encoding/decoding. Its generic
reader allocates from unchecked content-length and grows unbounded lines/bodies.
The candidate validates bounded input before library parsing: 4 KiB individual
header lines, 64 KiB total headers and 4 MiB bodies. Credentials are capped at
64 KiB. This inherited broker-input resource risk and candidate limits require
assessment with the native broker before a whole-service compatibility claim.
CONNECT headers are written raw after validation because the generic writer
escapes them, contrary to the protocol's CONNECT rules. Synchronous sessions
avoid adding asynchronous subscription queues and goroutine cleanup concerns.

Finite tests cover persistent receipt admission, mismatched receipts, callback
success/failure acknowledgement, six attempts, untrusted TLS, oversized/malformed
frames, binary framing, idle cancellation and actual native configuration keys.
The module's [Apache-2.0 license](stomp-LICENSE.txt) was inspected; scanner detection
passes the existing repository allowlist. Retain attribution/license text in the
eventual image. No third-party NOTICE file exists in this module.

All [22 unchanged original executions](paired/mls-upload-original.xml) pass:
[log](paired/mls-upload-original.log), [backend receipt](paired/result.json).
[Paired evidence](paired/paired-reads.json) also passes native-Go-native SQS
account/property deletion and acknowledgement, plus existing client/prekey/raw
and signed MLS durable controls. This uses Go Brig/Gundeck with Haskell remainder;
it is not the final eight-Go535 matrix or an actual STOMP broker qualification.
All25 managed processes exited0, runner/cleanup0; an
[independent resource audit](cleanup.json) found no remaining owned resources.

The [initial SDK failure](initial-sdk/build-receipt.json) and its logs are retained:
required YAML-node decoding was corrected in candidate code, and a callback fixture
received its missing mandatory cleanup dependency. The
[disk-floor preflight failure](preflight-failure.json) started no backend; it was
followed by a fresh successful namespace without lowering the guard.
[Historical SDK archive receipts](internal-stomp-lossless-maintenance.json) and
[additional artifact/fixture receipts](internal-stomp-additional-lossless-maintenance.json)
retain SHA-verified original bytes and ownership/mode restoration instructions.

Same-author review under the user's existing delegation accepts this increment.
Actual native ActiveMQ replay/redelivery and TLS/configuration comparisons remain
required, along with service-delete, other full-service families and final whole
Brig acceptance gates. Haskell sources/defaults and service stages are unchanged.
