# Gundeck inventory delegated self-review

Reviewed by codex (self-review delegated by user), same author under the user's
explicit autonomous migration/self-approval delegation and gundeck goal. Approved
for discovery only: source-cited API, old/new Redis schemas, Cassandra inline and
reference payloads, brig RPC, RabbitMQ/Cells effects, native token/SNS/SQS feedback,
config/operations/security/test gaps are covered. Whole-service scope includes
native providers; original event tests alone cannot qualify it.

Actual discovery command: docker run --rm --network none --read-only --tmpfs
/tmp:rw,noexec,nosuid,size=64m --user65534:65534 --cap-dropALL --security-opt
no-new-privileges --entrypoint /bin/bash sha256:f88496cd7850c4a32563e2085d41d2647e1038682a61fe0ffb5540c07f88ad7b
-c 'command -v gundeck-integration || true; integration --list --include PushToken
--include Notifications --include Events --include ConsumableNotifications'. Exit0;
actual output included PushToken.testRegisterPushToken and
PushToken.testVoipTokenRegistrationFails, all Events and Notifications required
cases, plus feature/MLS/Cells-related matches. gundeck-integration was unavailable.
No backend/test execution, no compatibility or AWS certification claimed.

Next actions change from generic notification-only migration to full provider
setup/contract capture. Haskell presence DELETE no-op, pre-v3 gap response, old
Redis fields, shared token endpoints and byte-limited referenced payload paging
must be mirrored. Native unchecked TLS behavior is not security-approved.

Validation: make migration-check migration-test passed (9 services/34 tasks; all32 validator tests). Source/defaults unchanged.
