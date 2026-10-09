# Federator inventory

Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. The next service is federator:
its forwarding boundary is focused and the private harness already provides DNS,
trusted mTLS ingress and three Haskell backends. Background-worker additionally
owns migrations and new PostgreSQL/meeting/adminless jobs, so is a broader step.

Source contracts: services/federator/src/Federator/InternalServer.hs exposes
GET /i/status[?standalone] and POST /rpc/{domain}/{brig|galley|cargohold}/{rpc}.
ExternalServer.hs exposes status and POST /federation/{component}/{rpc}, requiring
Wire-Origin-Domain and URL-encoded single PEM X-SSL-Certificate. No queries or
extra path segments. RPC.hs restricts RPC characters. Validation.hs checks policy,
discovers ALL origin SRV names and accepts a certificate matching any target name;
chain/expiry authentication is delegated to ingress. Outgoing Remote.hs discovers
the first SRV result and sends verified mTLS HTTP2; remote failures are wrapped,
success streams and adds Wire-Remote-IP. Service.hs forwards inward via HTTP1.1
with JSON Content-Type, origin, request ID, API version and validated origin IP;
only Content-Type survives in the inward response. ExternalServer.extractIp
handles forwarded/remote IP precedence. Discovery.hs distinguishes missing SRV422
from DNS errors400. Health.hs probes the other listener unless standalone.

Interpreter.hs retrieves federation policy from brig for EACH request; do not
invent a periodically refreshed cache. Options.hs/Run.hs configure two listener
ports, three local services, DNS resolver, CA roots/client certificate/key and TCP
connection timeout in microseconds. Warp binds by port (endpoint host not used).
Monitor/Internal.hs watches TLS files and retains previous context after invalid
reload. Metrics.hs maintains per-domain incoming/outgoing counters; common servant
middleware supplies /i/metrics and request-duration histograms. SIGTERM/SIGINT
stop both listeners and drain; whole-process switchback has no owned storage.

Actual discovery on 2026-10-09 in immutable runner
sha256:a21a2edacf0f3a48a68079b3450de4badd6a75623e2224d602e33669f5470ba0,
UID65534, network=none/read-only/capDropALL/no-new-privileges, command
`integration --list --include Federator --include Errors.testNestedError --include Demo.testDynamicBackendsFederation` returned exactly:

- Federator.testFederatorMetricsExternal
- Federator.testFederatorMetricsInternal
- Federator.testFederatorNumRequestsMetrics
- Errors.testNestedError
- Demo.testDynamicBackendsFederation

No federator-integration or federator-test executable is present on PATH. Their
source suites remain useful for finite supplemental characterization, not claimed
as executed. Existing original cargohold27 covers binary federation streaming,
missing assets/wrong domains/tokens. Existing Events/Notifications covers federated
messaging. Required final original matrix: these5 plus existing combined72 with
all five qualified Go services and remaining Haskell peers; baseline new5 first.

Finite supplements: exact health/routing/header/error/policy forwarding; missing
and failed DNS; incoming certificate mismatch/malformed/multiple/missing and
untrusted/wrong-host outbound TLS; streaming binary response/error envelope;
valid/invalid certificate reload; one bounded operations/drain and process
switchback using unchanged ingress/brig configs. Freeze concrete stimuli and
normalization in the contract task before candidate use. No exhaustive runtime
identity. Security concerns include trusting ingress headers on public exposure,
logging request bodies and DNS-selected destinations; document without weakening
checks. No production secrets or unsafe TLS parity exception.
