Scheduled runtime integration checkpoint, 2026-10-10

Base30bcc7f5e399f834391fb76ceac812ccebcfa075. Helper SHA256
ef026c79fa2aa4f9c97653613a28d631b654df26b65e36400affe9c307755990,
Go1.26.9/CGO_ENABLED=0. This is construction evidence, not a clean final worker
artifact or whole-service qualification. Helper/current input manifests identify the final construction files. Default
Go checks and the separately compiled/executed tagged fixture both pass on
these inputs. The earlier passing helper/checks before internal listener-ready
observability were added are retained under superseded-before-listener-readiness.
The final manual cron check waits for subscription, avoiding a lost-notification
startup race in the fixture. Actual exits recorded, no skipped check claimed.

Full default Go checks exit0: formatting, dependency verification, vet,
unit/race, build, vulnerability/license checks. Focused tests exercise concurrent
per-job capability Request-Id scoping and the native one-second missing-channel
wait/cancellation boundary. The tagged real PostgreSQL test exit0, all cleanup
exits0 and post-cleanup labels empty. Native schemas are readonly-mounted.

Combined fixture binds both pools to real handlers and PostgreSQL adapters:
expired meeting/code removal while keeping its regular conversation; adminless
setup and deletion traverse the real conversation and disabled-feature guard;
ACK removes their jobs; manual cron creates and executes another meetings job;
all five shared reaper gates initialize; native meetings counter increments;
both registry rows/liveness files disappear on cancellation with no DLQ.

Two failed fixture attempts are retained: initial setup lacked native history_depth
migration and wrongly expected a missing adminless conversation to ACK (source
correctly retries); second used another UUID's meeting code key. Setup now uses
the complete relevant native schema and a real disabled-feature conversation,
and hashes the actual meeting ID. No production behavior or protected assertion
was weakened to pass. Initial compile errors were field-name/interface wiring
errors corrected before checks; no passing evidence is attributed to them.

Source startup applies scheduled migrations before health. Native pools have
separate UUIDs, registry and configured capacity, shared NOTIFY, independent cron
and per-pool reapers with shared gates. Support threads and the reconnectable
notification publisher remain alive during both drains; the publisher's channel
retrieval waits at most1s, leaving four-attempt retry to the existing notifier.
Meetings caught batch failures still count as completed runs, as in the source.
Grace expiry warns/cancels scheduled jobs and does not force nonzero process exit.

Security: whitelisted/parameterized storage, strict persisted/notification payloads,
per-job request IDs with no mutable shared request state; credentials remain in
existing environment/file paths, no payload/raw backend errors logged. Context
cancellation bounds publisher connection teardown; existing verified TLS remains.
Only owned synthetic internal-network Docker, nonroot readonly cap-drop helper,
no host ports and explicit cleanup. No contract/security exception authorized.

Limits: this fixture has PostgreSQL-only routing, disabled adminless effects,
no live Cassandra, Brig, bots or Rabbit, and uses constructed pools rather than
Service.Run's full prepare/startup. Ordinary HTTP manager defaults still require
whole-service review. No fresh Haskell execution, original47/six-Go124 proof,
operations acceptance or actual Haskell-Go-Haskell durable switchback. Optional
image/selector and final delegated same-author review remain outstanding. Native
bot finite-DHE TLS and pusher discovery timing gaps remain explicit. Haskell is
unchanged/default; whole Haskell checks unavailable/unrun. Same-author construction
review uses the user's delegation, not independent review or whole-service approval.
