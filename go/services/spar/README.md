# Optional Go Spar (under construction)

The module implements configuration/Cassandra schema23 startup, health/lifecycle,
persistent settings/user metadata, SCIM token management, IdP CRUD/indexes and
SAML metadata/request initiation/state stores and connected signed-login/session
effects. SCIM authentication, Brig-backed groups and user create/read/delete are
implemented, along with user update/patch/filtered listing. Config/schema
metadata, internal cleanup and remaining finite SAML/SCIM contract gaps still
require completion.
Spar is not selectable or qualified yet; Haskell remains supported/default.

Use pinned Go1.26.9 with GOTOOLCHAIN=local/GOWORK=off. Run
`python3 go/check.py spar` from the repository root. Build `./cmd/spar` from this
module with CGO_ENABLED=0 for the owned Docker runner. The CLI accepts
-c/--config-file, default /etc/wire/spar/conf/spar.yaml. Startup connects to the
configured Cassandra keyspace and checks schema before listening; it does not
create or rewrite schemas. Single/multi-ingress configuration, TTL rounding,
Cassandra/DC/verified CA/discovery and request/cache policy follow native source.

Original Haskell tests run automatically in the owned private Docker harness,
with unchanged assertions and disposable dependencies. Current construction
evidence under qualification/evidence includes exact initial process comparisons,
six IdP/token originals, the original IdP issuer update/delete regression and
two original signed-login/UTF8 scenarios against actual Haskell peers.
Five unchanged original SCIM group scenarios also pass, including user invitation,
read and activation plus group search/pagination/update/delete/member guards.
Those batches prove only their listed surfaces. The final intended profile is
proxy-cargohold-cannon-gundeck-federator-background-worker-spar:225 executions.
Full trust/state gaps, operations smoke, durable Haskell→Go→Haskell switchback
and optional image/selector remain pending. See the [plan](../../../docs/go-migration/plans/spar.md)
and [frozen contract](../../../docs/go-migration/services/spar/contract.md).

Dependencies use the reviewed gocql2/YAML/x/text versions and goxmldsig1.6.1
with etree1.7.0. Signature tests reuse unchanged original Microsoft XML and
IdP/NameID goldens and cover pinned keys, expired certificates, the native ADFS
fallback, tamper/wrapping/unsigned assertions and duplicate credentials. Native
policy accepts legacy small RSA keys; the main binary's service-local compatibility
default preserves that acceptance without bypassing signature verification.
See [security notes](SECURITY-NOTES.md) for inherited concerns and unresolved
adapter gaps; tests of a signature helper do not qualify login/session security.
