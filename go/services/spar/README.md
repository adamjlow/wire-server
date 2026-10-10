# Optional Go Spar (under construction)

This is an initial runnable configuration/Cassandra/schema23/health/lifecycle
slice, not a selectable whole-service replacement. Complete SSO/IdP/SCIM behavior is not
implemented yet; the frozen whole-service acceptance still applies. Haskell
remains supported/default. No compatibility or deployment claim.

Use pinned Go1.26.9 with GOTOOLCHAIN=local/GOWORK=off. Run
`python3 go/check.py spar` from the repository root. Build `./cmd/spar` from this
module with CGO_ENABLED=0 for the owned Docker runner. The CLI accepts
-c/--config-file, default /etc/wire/spar/conf/spar.yaml. Original configuration
supports single/multi-ingress forms, Cassandra/DC/verified CA/discovery roles and
source limits/fingerprint syntax; future handlers will apply the TTL/state rules.

Startup actually connects to the configured Cassandra keyspace and reads its
source schema gate before serving. It does not create or rewrite schema. Ordinary
health and internal version guards, source cache policy/request IDs and graceful
SIGTERM are present. Config/database errors avoid payloads/credentials. Only the
existing reviewed gocql2/YAML versions are used; no new crypto dependency yet.

The qualification/early.py and run_early.py helpers execute a bounded actual
Haskell→Go process comparison in an owned internal Cassandra-only Docker fixture.
They compare HTTP/method/version/errors and clean SIGTERM, record exact identities
and require owned cleanup. This is not execution of original101 against Go or
proof of complete SAML/SCIM/storage behaviour. Actual reference originals101 and
the full contract remain in docs/go-migration/services/spar/.

The corrected early comparison passed at source9e209e02bdea478b84de16413c75c4e64e9e99b4: six exact native/Go HTTP responses, real schema23 startup, clean SIGTERM and owned cleanup. qualification/evidence/ retains the initial failure and corrected pass with exact identities. This qualifies only the initial process behavior, not the remaining service.

Persistent /sso/settings GET, /i/sso/settings PUT and /i/scim/userinfo/:id POST now use original Cassandra tables and query consistency. Setting an IdP checks its typed row first; certificate blobs use source XML KeyInfo, without imposing certificate validity-date policy. Missing SCIM metadata omits created_on. Expanded native/Go qualification passes eleven exact responses at source87deed0f0cdf2dff4d03a994ee22d15dc00ffda2, including empty settings, persistent clear, unknown IdP and missing SCIM metadata. Populated state, complete IdP/SCIM/SSO handlers and whole-service switchback remain unqualified.

SCIM token create/list/rename/delete management routes are implemented with Brig/Galley permission checks and creation reauthentication, crypto-random tokens and native logged dual-index Cassandra writes. Legacy plaintext conversion and v7/v8 response differences are preserved. Their selected original tests are prepared, not yet claimed passed. SAML/IdP CRUD and SCIM user/group behavior remain under construction.
