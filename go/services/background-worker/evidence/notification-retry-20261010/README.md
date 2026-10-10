Notification retry/discovery construction checkpoint, 2026-10-10

Base d3cdab13082ee715221cad076711317eb26d9e81. Full Go1.26.9 checks exit0:
format/tidy/verify/vet, unit/race/build, vulnerability/license checks. Source hashes
identify exact construction inputs, not a clean final executable or compatibility.
The earlier pass before exact-case queue field parsing is retained separately.

Wire.BackendNotificationPusher calls retry0.9.3.1's fullJitterBackoff, capDelay and
limitRetriesByCumulativeDelay. Retained native error stack identifies this package
version; tagged primary source was read and hashed, not freshly executed:
https://github.com/Soostone/retry/blob/0.9.3.1/src/Control/Retry.hs

Native delay is saturated exponential Int microseconds, divided by2, plus an
inclusive draw from0 through that half. Notification delay caps AFTER jitter;
sufficiently high attempts therefore have a fixed capped wait. Discovery uses
10ms base with at most60s accumulated waits, excluding HTTP time. Every failed
traversal restarts at page1, with no partial result retained; async cancellation
skips retry. Native traversal prepends each page's accepted domains and checks
the response page against response page_count, not the requested page number.

Focused candidate tests verify numeric endpoints/saturation, cap ordering,
deterministic13 attempts/12 waits/40.95s accumulated wait without actually sleeping,
cancellation before policy, real synthetic HTTP page2 failure and restart1/2/1/2,
source page order, basic authentication/request shape, >8MiB response acceptance,
required exact-case fields/types and signed integral Scientific page metadata.
Original Haskell assertions and schemas remain unchanged. Full-check race pass
includes the real HTTP test and synchronized observation of requests.

Source Page Queue requires items/page/page_count and both name/vhost. JSON is
strictly UTF8/surrogate-valid, required values cannot be null, exact field names
are respected, and errors do not contain credentials, raw response or queue names.
Unknown fields remain accepted. The artificial8MiB discovery cap was removed to
preserve native acceptance; inherited unbounded body/page accumulation is a
memory/availability concern. A new bound requires separately reviewed policy.
The retry BSD3 notice joins existing distribution notices; no Go dependency added.

This is source-based construction/HTTP proof, not fresh native policy execution,
original47/final six-Go124, real Rabbit-channel retry or complete worker startup.
No new Docker resources were started. Native DHE bot TLS, ordinary transport,
error diagnostic parity and retained SCIM500 setup failure remain unresolved.
Operations, actual durable switchback, image/selector and final delegated review
remain mandatory; goal/task active and unqualified, Haskell supported/default.
Same-author construction review uses the user's delegation, not independent review.
No Haskell changes; full Haskell builds unavailable/unrun. No remote action.
