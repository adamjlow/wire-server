# Bounded runner descriptor repair

Harness source `02fc9148aacd53b7bff23872c9f9d42f921ca707`, pinned publisher base
and unchanged static Go worker `12107801eab254cc61c87b88b8f9d2291cbd0558` are
identified in the receipts. This is fixture qualification, not whole-service parity.

Only the runner receives nofile soft/hard 8192. Its opaque TCP relay uses the
platform default selector to support descriptors above 1023, with unchanged
full writes, idle polling, EOF and half-close. Original assertions, catalogues,
normalization, synthetic TLS trust, nonroot/isolation and cleanup are unchanged.
The host harness checks pass 24 cases and migration checks pass 32 cases.
[Actual Docker regression](docker-probe.json) proves the bound and a multi-chunk
high-FD request/response with client half-close in the new immutable image.

[Measured original result](result.json): 46/47 pass, no errors/skips; runner1,
controller2, cleanup0 and no remaining owned containers/networks/volumes.
Handle-dispute and reindexing cases now pass. Main user migration still fails its
phase1 SCIM deletion GET, expected404/actual500 (User.hs194; runPhase1 at139).
Phase1 selects Cassandra and migrateUsersfalse; this matches a previously
retained failure, but is not waived or attributed to native Haskell without a
matching reference run. The exact [XML](original.xml) retains every assertion.
Its failure context contains synthetic test users/passwords/tokens only.

[Readonly resource samples](monitor.jsonl) show runner FD high-water1702,
threads570, RSS150052 KiB; cgroup peak1574649856 bytes and sampled PID peak1205.
No observed OOM or PID-limit events. Five-second sampling can miss higher peaks
and adds one Python observer. Current demand exceeds the prior1024 limit; the
old failed run was not monitored, so individual failure causality is unproven.
Raw native/dependency logs remain in the external cache identified by hashes
in result.json; monitor source and Docker regression source are retained here.

Final six-Go124, operations, actual durable Haskell-Go-Haskell switchback,
packaged patched OpenSSL image/selector and whole-service review remain pending.
No Go/Haskell source, service defaults, availability or compatibility changed.
