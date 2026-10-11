# Ordered queued service deletion core

Source `6418916bc31eb064f28e9a743e654f7bee57ba84`, service
`ada3a0469c538fcf24e69b8caf8ae3df67d40bd41afbda5560841d76f9a14b1b`:
[pinned Go checks](go-check.log), [static original-runner units](runtime-units.log)
and [exact build receipt](build-receipt.json) pass. Tagged stores compile only.
SDK/static-runner/cleanup exit0; independent container/network inventory is empty.
Retained [controller output](controller.log) includes the existing glibc static
linker warnings; no new runtime coverage is inferred from compilation.

The deletion core follows native finishDeleteService and deleteBot effect order:
lookup service, page bot/conversation pairs, at most16 concurrent removals per
page, disconnect Galley service, then delete service/index/whitelist metadata.
Bot removal first calls Galley, looks up account and clients, directly removes
client/prekey/lock state, removes its service association, and marks status2.
Pending invitations do not supply an account service reference; no full ordinary
account-deletion cascade runs. Missing service is an idempotent replay.
Finite tests check every bot-phase failure stops subsequent effects,17-bot worker
saturation/peak16/completion-before-disconnect, and failed bot pages retain final
metadata. The production context propagates cancellation; a dedicated canceled
page control remains pending with real adapters.

This core is deliberately **not bound to the service.delete queue callback**:
real native service/bot stores, mixed pagination, Galley RPC adapters and actual
native-Go-native replay must be implemented and qualified. No success stub or
service compatibility claim is introduced. Original backend22 evidence remains
at its earlier exact service tuple; no new backend e2e was run for this binary.
Same-author review under the user's existing delegation accepts this increment
only. Whole535/eight-Go/all-entity/operations/image/selection gates remain pending.
