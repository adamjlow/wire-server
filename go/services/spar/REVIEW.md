# Spar completion review

Reviewer: codex (same author; self-review explicitly delegated by the user in
this session). Approved for the finite qualified optional whole-service contract.
This is not an independent security audit or production deployment approval.

Reference/upstream source: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Candidate source: `40c1b2f896fe20d5d9aad4a7c90a815fcfa0e067`.
Static Go1.26.9 executable SHA256:
`9fabe438a2b52f723d698168350d90cb1157dc43f130803e841a7b224a884d42`.
Optional image: `sha256:ce3965bfed8180a59d04cf9e31a154dd68b8f4312dbb1ab6f1e85dd6b0bd0132`.
Metadata-only completion commits do not change these runtime inputs.

The production CLI connects directly to Cassandra and Brig/Galley. It supplies
SSO metadata/initiate/finalize and web/mobile verdicts, IdP CRUD/replacement,
SCIM token management, SCIM discovery/user CRUD/PATCH/filtering, group CRUD,
settings, email discovery and internal user/team cleanup. There is no embedded
Haskell runtime or successful-path placeholder. The native undefined Groups
PATCH remains unsupported: actual valid PATCH yields the generic outer500;
wrong media yields415; the characterized malformed body yields native plain400.
The original API versions, config/default path, multi-ingress/allowlist,
schema23 check, key/TTL formats, legacy/v2 mappings and nontransactional native
write sequence remain supported. Production initialization constructs the real
Cassandra store before installing the handlers; test-store interfaces do not
replace configured production behavior.

| Acceptance | Evidence under `qualification/evidence/` | Decision |
| --- | --- | --- |
| Frozen native contract/original baseline | `../../../docs/go-migration/services/spar/contract.md` and its `oracle/`; `switchback-final/source-audit.json` | 155 reference inputs byte-identical; unchanged compiled native101 baseline passed. No Haskell/library/integration/schema changes. |
| Final Go quality/dependencies | `group-patch-native-corrected/go-check.log`, `build.json`, `image-build.json` | Pinned1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license pass; final static artifact exact9fabe438. No patched dependency forks or scanner waiver. |
| Entire original seven-Go/Haskell matrix | `final-original225-batched/result.json` and six complete XML/run reports | All225 required executions pass strict original names/XML, no failures/errors/skips/duplicate names within a suite. All seven Go artifacts, remaining Haskell executable hashes, runner, synthetic integration config and dependency digests identical across batches. |
| Native HTTP/middleware/metrics | `middleware-native-corrected/construction-checks.json` | 33 existing replies plus11 gzip/readback/metrics probes match. Runtime GC/latency identity and predeclared incidental parser diagnostics excluded, never successful auth/state behavior. |
| Native XML signatures/forms | `signature-native-corrected/`, `forms-native-corrected/`, `switchback-final/proof-reuse.json` | 12 signed plus9 tamper controls,33 existing replies and12IdP reads;20 forms plus33 existing replies pass. Exact relevant signature/metadata/form/gzip/metrics inputs unchanged at final40; proof reuse explicitly audited. Connected original101 and current switchback exercise final login dispatch/state. |
| Native unsupported Groups PATCH | `group-patch-native-corrected/{result.json.gz,environment.json}` | Actual authenticated native/Go three-case status/header/body agreement, no mutation/session/cookie; quality passes, native/Go shutdown0, fixture/runner/cleanup0 and independently empty project. Initial differences retained. |
| Exact image operations | `operations-image-final/result.json` | 100 requests/concurrency8, exact34health/33settings/33SCIM-config counters; p95 .009s, RSS12.5MB, CPUdelta .02s within declared budgets. Eight accepted held settings PUTs complete200 after listener closes; SIGTERM exit0 in .168s within30+5s. Nonroot65534, existing config, trusted CA roots and owned cleanup verified. Finite smoke, not production capacity/SLO. |
| Actual durable rollback | `switchback-final/{result.json.gz,environment.json,build.json}` | Native→Go→native over one unchanged Cassandra/Brig/Galley state;102 HTTP/CQL actions and seven snapshots. All three owners clean SIGTERM0, only one Spar owner at a time, helper/runner/cleanup0 and independently empty project. |
| Optional image/selection | `group-patch-native-corrected/{image-build.json,selector.json}` and `operations-image-final/selector.json`; separate `spar-selector-contract` task | Static scratch65534:65534 image has native config path,167 trusted reference CA certificates, licenses and corresponding source. Actual Helm default bytes unchanged; only selected Spar image line changes. RollingUpdate/config/probes/env/peer images retained; six fail-closed CLI inputs verified under unchanged selector. No push/deploy. |

The225 pass is **batched**, not a claim that an earlier combined run passed:
proxy5+cargohold27 from `seven-go-original225-current`, Spar101+gundeck40+
federator5 from `seven-go-original193-remaining`, worker47 from
`seven-go-worker47-supervised`. First two overall runs failed later, respectively
at an unexpected earlier client broadcast and native Galley533 during MLS setup.
Only their complete passing suites qualify; the partial worker46 does not.
`retain_original225.py` rechecks the unchanged original catalogues/shared strict
XML checker, complete run exit0/actual peer identities, dependency/config/runner
identity and independent empty owned resources. It never merges/filters XML or
changes assertions. Counts are executions; overlapping names across different
suites are allowed by the unchanged catalogue. Cannon is selected/exercised as
a peer, without inventing an additional Cannon primary suite.

Retained initial full runs also exposed native port-release failures and a
conversation-code deletion200/404 mismatch. Bounded unchanged native/Go code5
and client-specific native/Go probes passed; those mismatches did not reproduce
and their cause remains unproven. The isolated worker47 failure concretely showed
orphan native Nginx workers still listening after the original Haskell test
cleanup's SIGINT/50ms/SIGKILL-master sequence. Separate completed protected task
`e2e-nginx-orphans` adds an opt-in nonroot/private-PID1 supervisor for **adopted
native Nginx workers only**, PID identity/UID/executable checked through pidfds,
SIGTERM/reap, original coordinator result decisive. Its actual two-Nginx proof
reaped the deliberately orphaned worker and preserved the healthy instance;
all27 existing harness tests passed. Final worker47 used that option and passed,
**with zero supervisor signals**. It prevents the demonstrated orphan condition;
the successful run does not prove that intervention caused success. Haskell
production/test code, original assertions, catalogue and deadlines were not
repaired or weakened. Raw failed reports remain linked in the plan/evidence.

Rollback verifies native-created full IdP/token/users/groups/weak versions/meta/
external mappings and Brig accounts are readable/updatable in Go; historical
unnamed plaintext token conversion remains reusable in returning native. Native
pending web/mobile requests complete in Go and Go pending requests complete in
returning native; assertion replay is denied in both directions. Actual invalid
mobile path and native lax query/cookie encoding agree. Populated native-team
cleanup in Go deletes native IdP/tokens/SAML mappings while retaining native
SCIM metadata/external/Brig semantics. Returning native deletes Go-updated
resources; snapshots and CQL emptiness verify outcomes, rather than generated
HTTP expectations alone. The first/return-native process logs use one driver
log path; the102-action record/owners/snapshots retain both phases, without a
claim that a separate first-phase process log exists.

Trust review: parameterized CQL and validated identifiers/payloads; verified
Cassandra CA/hostname; secret-free driver diagnostics and synthetic fixtures.
Namespace-aware XML rejects external entities/multiple roots/ambiguous IDs;
only configured stored signing keys validate the referenced assertion subtree.
Response KeyInfo is not trusted and external reference URLs are not fetched.
Request/issuer/team/audience/recipient/timestamps/replay checks remain mandatory.
The adapter uses maintained goxmldsig1.6.1 canonicalizers and standard RSA plus
Decred RIPEMD1601.0.2, without copying a fork. Forms stream without temporary
files; finite no-auth-effect/bounded-read regressions exercise parser rejection.
Image notices include the inherited SCIM schema licenses and dependencies.

[Security notes](SECURITY-NOTES.md) record serious inherited policy concerns:
small RSA keys/SHA1/expired stored certificates, replacement ownership source
concern, issuer-wide cross-team cleanup, debug/postMessage/privacy surfaces,
plaintext legacy tokens, nontransactional/replay races, unlimited native file
size and session issuance before an invalid mobile redirect fails. These were
preserved/documented under the explicit mirror-current-backend instruction;
parity/self-review grants no security exception or silent policy repair.

Native unit executables and a full native rebuild are unavailable; none is
claimed passed. Original compiled Haskell e2e tests ran in the owned Docker
harness without host toolchain installation. The finite proof does not establish
exhaustive malformed-input diagnostic byte identity, all possible load/network
faults, production capacity, or independent audit. Future upstream changes
require compatibility reassessment under the existing sync policy. Haskell stays
supported/default; availability means only the optional reviewed service choice.
All finite implementation criteria are satisfied; separate verification and
selection gates may now publish legal adjacent availability transitions.
