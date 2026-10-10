# Brig service inventory

Reference/upstream `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`; source inspected
on go-migration. This is discovery, not a Go implementation or compatibility
claim. Haskell remains supported/default. Exact original compiled discovery,
selected source hashes/function mapping and unavailable executable probes are in
[test-discovery.json](test-discovery.json).

## Boundary and entrypoints

`services/brig/src/Brig/Main.hs` parses -c/--config-file, native default
`/etc/wire/brig/conf/brig.yaml`, default/run and migrate-postgres with optional
--dbname and explicit --reset. `Brig.Run.run` constructs the environment, runs
PostgreSQL migrations, starts internal events, optional SES feedback, TURN/SFT
discovery, auth metrics and pending-activation cleanup before serving. Shutdown
cancels these tasks and closes the environment. Go must implement the configured
service and this CLI migration contract; a health-only executable is insufficient.
Existing standalone brig-schema and brig-index are separate operator utilities;
keep native workflows working. Porting those utilities is not a prerequisite to
using Go Brig, but never embed/call the native brig service to supply Go behavior.

`Brig.Run.mkApp` composes version rewrites, compatibility rewrite
/i/users/handles→/i/handles, request IDs, Servant metrics, gunzip, response gzip,
error handling and tracing. Service API comprises docs, public BrigAPI, internal
API, /federation BrigApi and version API. `Brig.API.Public.docsAPI` also renders
public/internal/federated Swagger schemas for peers: include served documentation
and version/error serialization in the contract, not just business endpoints.

| Complete family | Authoritative routes/handler sources | Required behavior |
| --- | --- | --- |
| User/self/account/handles | libs/wire-api/src/Wire/API/Routes/Public/Brig.hs; Brig.API.User; Wire.UserSubsystem.Interpreter | Register/activate/update/lookup/status/delete, SSO/SCIM identities, email/locale/assets/rich-info/protocols, handle allocation/filtering and privacy/version projections. |
| Authentication | Brig.API.Auth; Wire.AuthenticationSubsystem.{Interpreter,ZAuth,Cookie}; libs/zauth/src/Data/ZAuth/{Token,Creation,Validation,CryptoSign}.hs | Password/SSO login, persistent/session/legal-hold cookies, access renewal/logout/revocation/labels/limits, verification/reset/re-auth and exact signed ZAuth bytes/key rotation/expiry. Preserve cookie rows and token type/client binding. |
| Clients/prekeys | Brig.User.Client; Wire.ClientSubsystem.Interpreter; Wire.ClientStore.{Cassandra,DynamoDB} | Client identity/capabilities/last-active/MLS public keys, device limits/re-auth, legal-hold gating, validate/claim last-resort/one-time prekeys, federation and Dynamo optimistic lock or configured randomPrekeys local-lock branch. |
| MLS/E2E identity | Brig.API.MLS.{KeyPackages,KeyPackages.Validation,Util,CipherSuite}; Wire.MlsKeyPackageSubsystem.Interpreter; Wire.JwtTools | Cipher-suite, signature/identity/lifetime validation, package upload/count/replace/claim/reference mapping/deletion, local/federated clients, nonce and DPoP enrollment. Rust JWT helper defines native behavior; Go must implement that contract without Haskell FFI. |
| Connections | Brig.API.Connection; Brig.API.Connection.{Remote,Util}; Brig.Data.Connection | Local/remote state transitions, blocks/consent/legal-hold, limits/paging and Galley one-to-one effects/events. |
| Properties/user groups/apps | public Brig UserGroupAPI/AppsAPI/PropertiesAPI; Wire.{PropertySubsystem,UserGroupSubsystem,AppSubsystem}.Interpreter | Storage/size/member/channel/team/permission rules, paging/events, app auth/cookies/IDs and deletion. |
| Search | Brig.User.Search.{Index,Types}; Wire.IndexedUserStore.ElasticSearch; Brig.API.Public | Native index documents/versioning, visibility/team/handle/filters/paging, dual-index writes and Galley feature lookup. |
| Team/invitations/collaborators | Brig.Team.API; Wire.TeamInvitationSubsystem/TeamSubsystem; Wire.TeamCollaboratorsStore.Postgres | Binding registration/invitations/permissions/status/email/journal effects and configured feature checks; Galley remains conversation/team reference. |
| OAuth/provider/services/bots | Brig.API.OAuth; Brig.Provider.API; public route submodules | Authorization/code/PKCE/access/refresh/revoke/client registrations, provider credentials/reset/activation, service metadata/keys/fingerprints/discovery/connections, bot/client/account/conversation effects. |
| Calling/system settings | Brig.Calling.{API,Internal}; Brig.Calling; Brig.API.Public | TURN HMAC credentials/TTL/server ordering/file or DNS discovery, SFT discovery/static/all-server config, system config responses and supported versions. |
| Enterprise/domain verification | public Brig.DomainVerification route modules; Wire.{EnterpriseLoginSubsystem,DomainRegistrationStore,DomainVerificationChallengeStore} | Feature/team/admin checks, DNS/HTTP challenge/proof/TTL, SSO redirects/registration/custom backend flows and storage mode semantics. |
| Internal API | libs/wire-api/src/Wire/API/Routes/Internal/Brig.hs and submodules; Brig.API.Internal | Health/EJPD/accounts/users/teams/client/MLS/auth/OAuth/search/federation-remotes/provider/enterprise/SAML/app routes, peer-facing projections/auth conventions and legacy handle rewrite. |
| Federation API | libs/wire-api-federation/src/Wire/API/Federation/API/Brig.hs; Brig.API.Federation | API version, user/handle lookup, prekey single/bundle/multi claims, search, MLS clients/packages, connection actions/backend connectivity/deleted-user notifications; origin-domain/allowlist/team checks. |

The public BrigAPI explicitly includes24 families: User, Self, Account, Client,
Prekey, UserClient, Connection, Properties, MLS, UserHandle, Search, Auth, Calling,
Teams, SystemSettings, OAuth, Bot, Services, Provider, DomainVerification,
DomainVerificationTeam, DomainVerificationChallenge, UserGroup and Apps. Route
schemas and version gates in wire-api are authoritative; freeze named route
catalogues and relevant models under the next protected contract task.

## Runtime dependencies, state and configuration

`Brig.Options` and `Brig.App.newEnv` define Cassandra, PostgreSQL/pool/password,
Elasticsearch plus additional-write index/url/CA/credentials, AWS Dynamo/SQS/SES,
optional SMTP, RabbitMQ, optional STOMP, ZAuth private/public key files, TURN
secret/source, optional SFT, local federation domain/strategy/remotes and HTTP
peers Galley/Gundeck/Cargohold/Spar/Federator. Template files/branding/locales,
feature/password-hashing/rate-limit settings, TTLs, disabled versions and
migration switches are part of deployment compatibility. Avoid treating the
integration configuration as the only supported configuration.

`Brig.App.initCassandra` checks `Brig.Schema.Run.lastSchemaVersion` (94, after
V94_ReduceUserGCGracePeriod); service startup checks existing schema rather than
running Cassandra schema migration. `Wire.PostgresMigrations` supplies SQL
migrations run at service startup and migrate-postgres. SQL/checksums and
existing names/types must remain unchanged. `Brig.CanonicalInterpreter` selects
Cassandra, PostgreSQL or Cassandra+PostgreSQL dual-write for user, block-list,
domain-registration/challenge and password-reset stores. PostgreSQL also stores
apps/user groups/team collaborators; Cassandra remains needed for other stores.
All three configured storage modes require source-level construction/regression
coverage and real durable switchback state, not only the default Cassandra mode.

Cassandra stores include user/password/user keys and handles, activation/pending
users, session/cookies, verification/reset/budgets, clients/prekeys/capabilities,
properties, connections/local+remote indexes, MLS packages/reference mappings,
federation remotes, providers/services/OAuth/nonces and SSO/SCIM-linked identities.
Shared subsystem stores/schema queries are authoritative. Preserve identifiers,
password tags/parameters, token/key material encodings, TTL/writetime/delete and
nontransactional ordering, old/new nullable fields and domain-qualified keys.
`Wire.HashPassword.Interpreter` verifies stored Scrypt/Argon2id in constant time
and marks Scrypt for upgrade only under configured Argon2id policy. No blanket
password-format rewrite on migration.

Internal event queue is SQS or configured STOMP; SES feedback is optional.
`Brig.InternalEvent.Process`, `Brig.AWS.SesNotification`, `Brig.Queue` define
ACK/retry/delivery/cleanup; Gundeck notifications, AWS user-journal and Rabbit
backend queues/background job publishing have existing routing/payload semantics.
Pending-activation cleanup and device/account removal must retain remote, search,
queue and Spar effects. MLS package claims use process-local locking;
prekey claims have the source best-effort Dynamo optimistic lock and retry branch.

Helm `charts/wire-server/templates/brig/deployment.yaml` uses RollingUpdate,
maxUnavailable0 and configurable maxSurge; existing config/secrets/TURN/CA mounts,
service account, probes/security/resources/env are part of optional selection.
Selector must leave omitted output byte-identical and change only explicit Brig
image; default Haskell targets/images/development do not require Go tooling.

## Reusable test evidence and finite gaps

Actual restricted nonroot/network-none runner099b3065 (harness source890a4742)
executed integration --list and brig --help successfully; native Brig executable
SHA c45a40da63268712a7cacdbd884287fa3dc93cec179d70323d3f22f7d5cdf1ca.
It listed1,001 unique execution names. Proposed Brig selection is310 executions
(260 distinct base functions), all mapped to current source definitions, with
boolean/API/protocol variants preserved. The discovery JSON retains exact names,
source hashes, raw list/help, schema/index/integration probes and removed-container
check. No backend was started and **no integration test pass is claimed**.

Selection covers35 core/auth/client/account/enterprise/OAuth/provider/search/
team/app/group/MLS/migration modules, plus conference-calling/user feature,
Shape/Swagger/Version modules and19 legal-hold client/prekey/connection/login
executions. The detailed list is authoritative for this proposal; freeze it in a
protected task before executing. Selected originals complement the existing225
seven-Go peer executions; proposed eight-service total is535 executions across
complete suites, retaining deliberate overlaps across suites. Discovery does not
establish candidate support or that every selected case has a runnable baseline.

brig-integration and brig-tests/GHC/Cabal/Nix are absent in the published runner.
37 legacy native source test files remain reusable **expectations**, not executed
native units. Calling/source discovery, richer prekey/rich-info/account/metrics
coverage and token/hash/DPoP goldens require focused finite native/Go probes or
Go regressions drawn from originals. No host installation/full native rebuild.
Do not run every unrelated conversation test merely because the binary contains
it; keep exact service matrix and genuinely uncovered trust/state cases.

Contract task must predeclare finite controls for: middleware/config/schema/CLI;
ZAuth/signature/cookies and both password formats; prekey consume/last-resort/
concurrent claim; MLS/DPoP tamper/identity/expiry; peer-origin/team permissions;
queue ACK/retry/idempotence; source search/dual-index serialization; TURN/SFT
original ordering/HMAC/discovery; all configured storage branches; served Swagger
and source metrics. Successful auth/storage paths must use real native and Go
processes; local mocked expectations supplement rather than replace them.

Operations acceptance:100 representative requests/concurrency8,30s wall budget,
p955s, CPUdelta10s and RSS256MiB; eight accepted held bodies drain after SIGTERM
with listener closed before releasing bodies, exit0 within native configured
grace+5s. Characterize exact endpoint/status/counters and native shutdown grace
in the contract before execution. Finite smoke, not production SLO/capacity.
Switchback: one owned state namespace/one owner at a time, native creates real
accounts/password hashes/session/clients/prekeys/connections/properties/packages/
OAuth/apps/groups; Go reads/updates/consumes and creates new state; returning
native authenticates/resumes/reads/consumes/deletes Go state. Preserve pending
activation/reset and queue redelivery, index effects and storage-mode witnesses.
Freeze the bounded action/snapshot matrix before executing; do not claim a
response-only fixture proves rollback or require every unused table row.

## Serious inherited concerns and unresolved decisions

* `Brig.Run` explicitly notes unsupervised async threads can terminate while the
  service continues serving. Source concern; no production failure reproduced.
* `Brig.Options.ElasticSearchOpts` documents possible retained deleted users with
  additionalWriteIndex; reproduction is uncertain. Retain source write/delete
  order and document concern; don't repair Haskell/search behavior.
* Configured Elasticsearch insecureSkipVerifyTls/additionalInsecureSkipVerifyTls
  invoke `SSL.VerifyNone` in Brig.App. Defaults/internal client verification must
  be preserved; an explicit inherited insecure setting is not migration security
  approval. The source comment about historical TLSv1 AWS support does not prove
  the published OpenSSL currently accepts TLSv1.
* ClientStore.DynamoDB calls prekey locking best-effort; package locking is local
  to a process. Do not promise global single-use atomicity beyond the source.
* Authentication password-reset retry rewrites whole TTL rows to avoid a nullable
  retry-only race; credential/TTL/write ordering needs paired state tests.

ZAuth and Scrypt/Argon2id, JWT/DPoP native Rust behavior, MLS/proteus serialization,
email/template output and TURN/DNS policies require explicit library/adapter
choices after frozen contract review. Do not choose a maintained Go library's
defaults as the contract. Pinned hashes/source and native comparisons establish
compatibility. No discovered blocker justifies shrinking the whole-service goal.
