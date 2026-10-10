# Private runner descriptor capacity

Known fixture issue: the pinned runner starts with soft RLIMIT_NOFILE1024. The
current clean Go worker original47 run passes44 and fails3 user cases with native
CQL/Elasticsearch/Postgres connection closures. A scaled original-Forward Docker
probe reproduces caught EMFILE followed by EOF; actual failed-run FD high-water was
not captured, so attribution remains unproven. Source select.select also cannot
handle descriptors above1023. This is a distinct bounded harness repair; the Go
implementation and all original47/combined124 assertions remain unchanged.

Use a finite runner-only nofile soft/hard8192 ceiling and DefaultSelector for the
same byte-preserving TCP relay, retaining idle polling, full writes, EOF and half-close
behavior. Preserve nonroot/read-only/internal networks, no host ports, CPU/memory/PID
bounds, source test catalogues/normalization and mandatory owned cleanup. No fixture
bypass, TLS relaxation or service/default change. No Go source edits on this branch.

Run existing Python harness/isolation tests plus one real high-FD relay regression
with normal and half-close exchange; observe the actual bound inside owned Docker.
Build a new immutable runner from unchanged publisher base/current harness inputs.
Rerun exact original47 with the unchanged clean12107801e worker, capture runner FD/
thread/RSS/pid/memory high-water using a readonly external monitor, retain all strict
XML results/failures and exact identities. Passing fixture checks never waive a
failing candidate case. Do not repeat service builds for unchanged Go code.

Keep results/review under docs/go-migration/qualifications/background-worker-harness-fd/.
Commands: Python unittest discover tools/parity/docker; make migration-check
migration-test; scoped tools/migration/check.py against claimed base; existing
control.py build and environment.py prepare/run with the identified candidate.
The final same-author review uses the continuing explicit user delegation for this
migration/harness work; label it honestly. Whole-service/final-six-Go/operations/
actual durable switchback/image/selector qualification remains separate and pending.
