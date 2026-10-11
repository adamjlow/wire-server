# STOMP native scalar admission correction

Source `aadef862b8944598a50ce140595d02b27ee3c671`, service
`51e309c195c2332c29675e59fbd5698980960cfea9f4cfcf6b443d6274118f56`:
[pinned Go checks](go-check.log) and
[static units in the unchanged original runner](runtime-units.log) pass.
The [build receipt](build-receipt.json) identifies compiler, runner and artifacts;
[tagged stores](stores-build-receipt.json) compile but were not executed here.
Controller terminal exit and cleanup are zero. An independent Docker inventory
found no running containers or matching owned networks after completion.

The correction prevents yaml.v3 numeric-to-string coercion and fractional-port
truncation. Finite tests cover native YAML scalar classification, exact integral
ports without floating-point rounding, credential types, aliases/merges and
recursive-input bounds. Existing credential framing and file limits remain.
The parser follows inspected native Data.Yaml source; compiled-native admission
confirmation remains pending. This is candidate unit/build evidence, not a new
native configuration parity or backend e2e claim.

[Previous actual ActiveMQ qualification](../internal-stomp-activemq/README.md)
remains tied to its older service42ea8a76 executable. The revised service needs
its affected runtime controls and final whole-service qualification. Static linker
warnings about glibc dlopen/name resolution are retained in [controller output](controller.log);
static unit execution passed, which does not establish every dynamic runtime path.

Same-author review under the user's existing delegation accepts this bounded
increment only. Brig remains incomplete; no service milestone, Haskell default,
protected oracle, production switch or whole535/eight-Go qualification changes.
