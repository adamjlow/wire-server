# Exact-image operations check (2026-10-10)

The declared single smoke passes on worker source89f24e7a, binary SHA706fed84952a9ee80dc1c9ac7b670d7417114cfe93b835d6d5fe94e58e2628d6,
image sha256:a14d2033f771f8849c604028709c6745e0f8a682c8da60d1a616dba1639dd655.
Qualification scripts/source revision368aeb414 are recorded in result.json.
No shipping Go source changed. The qualification-only static h2c sink is separately
hashed; its build and vet and Python syntax checks passed. Existing service checks
and original124 evidence are reused for unchanged production inputs.

100 health200/empty requests at concurrency8 and five jobs of each supported type
pass in0.610s: received/started/succeeded each5, no new failed/invalid counters,
workersbusy0 and Rabbit empty. Missing groups/conversations are deliberate
source-supported successful no-op jobs. Health p95 is0.005515s. Actual process RSS
samples are12,443,648 and33,210,368bytes; CPU delta0.39s covers startup/smoke and
waiting for loaded delivery. These are samples, not peak or comparative benchmarks.

The unchanged image starts with both default config paths, UID65534, read-only
root/source, no capabilities/host ports and synthetic config in a private namespace.
Rabbit TLS uses the original trusted CA. One actual h2c notification is observed
unacknowledged while another is ready. SIGTERM exits0 in0.0595s against the35s
limit (configured30s grace plus5s); both unsent messages are then ready and none
unacknowledged. Candidate/namespace and owned dependencies are removed; cleanup0
and empty exact-project container/network/volume queries are retained.

This proves the declared operations/loaded-notification/image lifecycle surface.
It does not prove the outstanding full durable Haskell-Go-Haskell switchback or
selector and final review. No original suite is rerun, upstream schema/test edited,
production endpoint contacted, image pushed or Haskell default changed.
