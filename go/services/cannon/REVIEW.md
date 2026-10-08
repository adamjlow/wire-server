# Cannon delegated self-review

Status: APPROVED for the exact tested tuple and supported configurations below.
Review by codex, same author, under the user’s explicit self-approval/autonomous
migration delegation and request to complete cannon. This is not independent review.

Reference/upstream source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e;
qualified publisher runtime and Haskell executable identities are in the protected
oracle. Candidate production source 411f525e421d15e673870ace345462c4c4f80372,
Go1.26.8, CGO0, clean VCS receipt; binary
d9b31f435b19e43e8b8bd596311ffe03994f7027f23cfc63cfcad8fddeed4232.
Local optional image sha256:3e3fb5782d0054fe1788f0c6c6dc9895fd9c40c8092651b45ff4f130fe4e49cc
contains that exact binary. No remote push or production rollout.

Implementation review covers classic presence/await/raw push and ordered bulk,
generation-safe replacement cleanup, public version/principal/client parsing,
WebSocket frame/ack/full-sync/sync marker, RabbitMQ pool/prefetch/queue flags and
Cassandra LocalQuorum prepared parameterized recovery queries. Initial classic
slice was executable before events; no Haskell embedding or shared Go framework.
Temporary queues retain user and temporary bindings, client quorum queues retain
existing names/deadletters. Queued malformed notifications reject without requeue;
ack parsing preserves uint64 precision and requires the Boolean multiple field.
Cassandra schemas and gundeck routes remain unchanged.

Security: trusted TLS/hostnames, strict typed input, serialized socket writes,
bounded pool/fan-out, context/deadlines, generic backend errors and synthetic
fixtures. Go format/tidy/module verification/vet/race/build/module vulnerability
and license checks pass in [quality output](evidence/final-quality.log).
Concrete tests cover old-socket cleanup, binary/app/control frames, atomic bulk
validation with large integers, ack types/range, and rejection of untrusted TLS.
Established pinned dependency proof is retained in the protected oracle. Source
concerns (payload logs, TLS bypass, pathological drain settings, TTL unit ambiguity)
remain separately documented; no new schema/auth boundary or bundled hardening.
Supported qualification excludes insecureSkipVerifyTls=true: repository policy
requires TLS verification, so Go rejects it explicitly. Operators depending on
that option must retain Haskell; no unsafe-mode parity/security approval is claimed.
The legacy 17-byte reference capture challenge was corrected to standard16-byte
before Go consumption, as frozen in the contract. Invalid-length challenge parity
is not claimed. These limits are visible, not normalized away.

Finite evidence: all-Haskell original35 pass; first Go candidate34/35 fails on a
setup user.client-add event at a temporary socket ([retained review](evidence/candidate-1-review.md)).
First triple run fails in unchanged Haskell federator HTTP/2 connection handling
([retained review](evidence/triple-1-review.md)); it does not qualify cannon.
Neither failure was waived or used to change protected tests. Second triple run
passes proxy5/cargohold27 but fails two setup-notification cases in cannon; retained
focused Haskell control passes both. [Subscription diagnosis](evidence/focus-2-review.md)
identifies Haskell basic.consume nowait=True versus GoFalse. Matching that source
command sequence makes both original focused Go cases pass without sleeps, event
filtering, exclusive consumers or changed timeout/assertions. The subscription-corrected candidate passed all original67
([retained previous tuple](evidence/triple-3-review.md)); subsequent review found
reference-confirmed JSON compatibility corrections. Earlier binary evidence is
historical. [Final exact candidate](evidence/triple-4-review.md) passes all
original67: proxy5/cargohold27/cannon35, no failures/errors/skips; all three Go
services with remaining pinned Haskell peers, unchanged assertions/timeouts,
process0 and owned cleanup0. Ten HTTP records, seven config outcomes
and 28 actual frozen boundary records are the finite supplement; the reference
metadata’s29 count was a narrative typo, corrected in the separately reviewed
[cannon count correction](../../../.migration/reviews/cannon-count-correction.md).
Generated debug/startup diagnostic prefixes are incidental per frozen rules;
statuses, content types, frame opcodes/close reasons, provider effects and ordered
bulk values remain exact. Performance/drain use only predeclared bounds.

[Final supplemental comparison](evidence/supplements-qualified-review.json) passes
all 10 HTTP, 7 configuration and 28 frozen boundaries for the final binary.
[Direct JSON comparison](evidence/json-compatibility.json) confirms six actual
Haskell/Go outcome pairs: optional transient:null defaults false; zero in decimal
and scientific notation is accepted; exact maximum uint64 scientific notation
parses before the broker rejects its unknown tag; fractional tags retain the
failed-to-parse close. Exact decimal parsing avoids float rounding and bounds
resource use. Unit security negatives cover overflow, non-integral values, huge
exponents and wrong types.

[Real switchback](evidence/switchback-qualified.json) used synthetic persistent queue/Cassandra
state: Haskell-unacked1 redelivers in Go, Go-unacked2 redelivers in Haskell without
replaying acked1; abandoned full-sync survives both replacements and either
runtime’s ack removes the same row. All owned processes exit0; synthetic queue and
row removed. This is a real storage/broker test, not an in-memory mock.

[Helm selection](evidence/helm-selection.json) ran real Helm3.22 on disposable
upstream chart copies with synthetic values: only cannon StatefulSet’s cannon
image changes; nginz/configurator/config/secrets/args/original test image remain.
Omitted selection is byte-identical; malformed/ambiguous targets fail closed.
[Image smoke](evidence/image-smoke-qualified.json) used actual image, existing default config
path, UID65534, read-only root, dropped capabilities, private owned runner network
namespace/no host ports: health200 and SIGTERM exit0. Loaded shutdown and bounded
40-request smoke are separately in protected differential captures.

No Haskell source, default build, chart, schemas or protected test assertions were
changed by implementation. Native Nix/GHC/Cabal are unavailable on this host;
whole Haskell build/unit/format checks were not run or fabricated. Existing CI is
preserved. The actual unchanged original executable ran inside the authorized
private Docker setup. Qualification binds the retained source/runtime/fixture
identities and tested configurations; it does not prove every production load,
invalid config permutation or future upstream revision. The finite matrix is complete; no additional speculative tests are required.
Future upstream changes follow the existing sync procedure.
