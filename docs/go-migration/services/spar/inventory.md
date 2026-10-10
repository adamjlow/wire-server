# Spar whole-service inventory

Reference source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Discovery source checkout cd6c5f63572c2602edf32f2a7f8a0be143df1089; services/libs/integration unchanged from reference. No Go candidate or compatibility claim. Published Haskell runner/executable identities and actual compiled case discovery are in [test-discovery.json](test-discovery.json). This inventory defines the boundary; detailed response/error/storage contracts must be frozen separately before implementation.

## Entrypoint, configuration and operations

`services/spar/exec/Main.hs`, `src/Spar/Run.hs`, `Options.hs` and `App.hs`: CLI -c/--config-file defaults /etc/wire/spar/conf/spar.yaml. SAML spHost/spPort bind HTTP. Initialize Cassandra with lower-bound schema version23 before serving; create ordinary HTTP manager for Brig/Galley. `/i/status` returns NoContent (not a new dependency readiness probe). Warp shared shutdown, request IDs, source metrics, gzip request decoding, errors, API-version and SAML cache policy middleware are observable. No Rabbit worker or scheduled job runtime belongs to Spar.

Config: nested SAML log/bind settings plus either single spAppUri/spSsoUri/contacts or spDomainConfigs map (mutually exclusive forms); Brig/Galley endpoints, Cassandra endpoint/keyspace/DC/discovery/TLS settings, auth request/assertion TTL limits, SCIM token and rich-info limits, disabled API versions, absolute scimBaseUri, email IdP discovery toggle and optional SHA-1 certificate fingerprint set. Missing or empty fingerprint set disables it. See `libs/saml2-web-sso/src/SAML2/WebSSO/Config.hs` and `services/spar/spar.integration.yaml`. Schema tooling and historical offline data migrator remain existing Haskell operator tools; replacing the service must read/write their current state without requiring a new schema or runtime dependency on Haskell.

Chart `charts/wire-server/templates/spar/deployment.yaml` uses **RollingUpdate** (maxUnavailable0), unlike background-worker Recreate. Preserve existing Spar config and Cassandra mounts, probes/env/rolling settings; never reuse worker-specific selector assumptions. Operators may select a Go image only through an optional separately scoped selector.

## HTTP and effects boundary

Authority: `libs/wire-api/src/Wire/API/Routes/{Public,Internal}/Spar.hs`, `services/spar/src/Spar/API.hs`, `Error.hs`, `Scim/{Auth,User,Group,Types}.hs`, `libs/wire-subsystems/src/Wire/{IdPSubsystem,ScimSubsystem}/Interpreter.hs`. Follow handlers rather than stale comments: Groups are implemented even though a public API comment says unsupported.

| Surface | Behaviour/effects to preserve |
| --- | --- |
| SSO metadata | Legacy global and team metadata, Z-Host-selected ingress, XML issuer/ACS/contact serialization/cache policy |
| Login initiation | HEAD precheck/GET SAML AuthnRequest HTML form, IdP/team/feature access, redirect/cookie label validation and persistent request issuer/TTL/verdict |
| Login finalization | Multipart SAML response, legacy/team paths; actual signature-selected assertions, issuer/request/audience/recipient/time/replay verdict, cert allowlist, NameID/user/IdP migration/binding, Brig account and persistent cookie, mobile/web result/errors |
| SSO settings/email discovery | Default IdP; get-by-email fromV15, configured enablement/privacy/error and multi-ingress rules |
| IdP management | Get/raw XML/list/create/update/delete, team-admin/ownership/permissions, preV8 vs current semantics, version/issuer coexistence, replacement/old issuers/domain/multi-cert allowlist |
| SCIM token management | Wire owner/re-auth/code rules, random tokens/limit/IdP association, versioned create/list, name update/delete; persistent hash and legacy token conversion |
| SCIMv2 metadata/users | Config/schemas/resource types; bearer token→team, search/filter/pagination, create/read/replace/patch/delete, externalId/email/handle/SSO/rich-info/active/meta/ETag semantics, invitations and user lifecycle via Brig |
| SCIMv2 groups | Bearer-team isolation, search, group member validation and Galley-backed group create/get/update/patch/delete |
| Internal | Health, team deletion, pending SCIM user cleanup, default SSO settings, userinfo and team IdP list; maintain exact methods/status/media/errors |

## Durable state and dependency interactions

`Spar.CanonicalInterpreter` connects local Cassandra effects plus shared subsystem stores and Brig/Galley RPC. `Spar.Data` validates TTL>0 and bounded by configured max; preserve rounding. Schema migrationsV0–V23 are in `Spar.Schema.Run` and its imported modules. Current tables include authreq(req,idp_issuer) and authresp(resp) TTLs; verdict formats/redirects/cookie_label (twice auth request lifetime); SAML user/user_v2 identity mappings; idp/issuer_idp/issuer_idp_v2/team_idp and raw metadata; default_idp; team_provisioning_by_token/by_team; scim_external with creation_status; scim_user_times including email_type/email_primary. Historical bind_cookie/data_migration and retired tables must be distinguished from live access when freezing CQL, not recreated blindly.

LocalQuorum/prepared statements and source retries/batches/order matter. Request issuer lookup ties response to actual IdP; assertion IDs persist for replay detection. SCIM token lookup supports `sha512:` hash and legacy plaintext rows and converts legacy matches on access (`Spar.Sem.ScimTokenStore.Cassandra`, `Wire.API.User.Scim`). Preserve source key encoding and bidirectional deletion. Multi-ingress/team issuer identity and user-v2 paths are essential for coexistence. Switchback must keep existing IdPs, tokens, users/SCIM metadata, mappings, request/verdict/assertion state usable by both implementations. Account/group/session effects belong to existing Brig/Galley APIs; do not replace them with local synthetic success.

## Finite acceptance and concrete gaps

Actual pinned runner lists86 Spar executions across eight modules (including boolean variants), all base functions matched local source. Related cross-service SSO/SCIM functions are separately listed in discovery. Freeze that final selection and determine baseline fixture support in protected contract/harness work. Existing124 peer cases remain the six-Go mixed baseline; Spar adds its complete selected surface, with duplicate functions counted honestly.

Reuse originals for IdP/token lifecycle, SCIM user/email/group/invitation semantics, ordinary/UTF8/expired-cert SAML login, email discovery, issuer migration, ingress/cross-team/request mismatch, unsolicited response and fingerprint policies. Source unit/legacy service suites cover further API/XML/SCIM/TTL/error details but published native test executables/compiler tools are absent. Add only concrete uncovered request/assertion replay/TTL, XML signature wrapping/unsigned/tampered/XXE, invalid bearer/cross-team, version/config/error serialization and actual state switchback checks. Exact fixtures/case matrix are frozen before implementation, not an open-ended additional-test queue.

Early runnable comparison: native and Go config/health/method/version/errors/SIGTERM using owned existing harness. Final exact Go quality/security/license and original tests; one declared concurrency/latency/RSS/CPU/loaded-stop smoke; actual same-state Haskell→Go→Haskell with saved IdP/token/SCIM user and pending SAML request; optional trusted nonroot image/default-preserving selector. Missing test setup/cases fail explicitly. No production/default/push actions.

## Inherited concerns, not Haskell repair scope

- **Expired IdP signing certificates are deliberately accepted** at import and login; original `Spar.testSparExpiredIdpCertStillWorks` asserts this. Go must preserve this policy; do not blindly apply generic web-PKI date validation. Future certificate lifecycle changes are separate security policy work.
- Configured certificate allowlist uses SHA-1 fingerprints; empty disables it. Preserve exact policy while evaluating independent mandatory signature verification; fingerprint membership alone cannot authorize an assertion.
- `Spar.Run.mkApp` enables heavy request-body debug logging for finalize-login; SAML payload can contain personal/authentication data. Document source logging policy and prevent new credential leaks; any policy change needs separate review.
- Local replay store performs read/store operations separately (`SAML2.WebSSO.SP.storeAssertion`); characterize actual concurrency/consistency before claiming an atomic anti-replay guarantee. This is a potential inherited race, not a proven exploit or authorization to rewrite storage.
- Legacy plaintext SCIM token conversion is supported; retain safe secret handling and migration compatibility. Internal endpoints rely on existing network boundaries; do not silently invent authentication.

Unresolved before contract freeze: exact SAML signature/canonicalization/algorithm formats, error/redirect and cookie wire details; current table usage/codec and cleanup ordering; related-case baseline fixture reachability; unit-source gaps; image trusted CA/config/startup compatibility. These are finite contract discovery items, not permission to ship an incomplete service. No tests have executed during this inventory.
