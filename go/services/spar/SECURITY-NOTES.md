# Inherited Spar concerns

The optional candidate has completed its finite parity qualification and delegated
same-author [review](REVIEW.md). Preserve reference behavior and record concerns; no Haskell repair or security-policy change is part of this migration.
The frozen contract already records signing-certificate date/fingerprint policy,
login debug data exposure and replay read/write races.

Source inspection found another ownership concern in
`services/spar/src/Spar/API.hs:validateNewIdP` and `idpCreate`: the supplied
`replaces` IdP is loaded to obtain its issuer/history and then its `replaced_by`
field is updated. These functions contain no explicit check that the replaced
IdP belongs to the requesting admin's team, despite the nearby comment describing
that requirement. The outer admin check applies to the requesting user's team.
Potential effects include changing another team's replacement/provisioning state
if its IdP UUID is known. This is a source-level concern, not a demonstrated
exploit; the complete native authentication effects do not establish an exploit. Go
follows that source sequence; original authentication/team cases pass. Do not silently add an ownership
policy or infer approval for a security exception from parity/self-review.

XML metadata parsing uses namespace-aware standard Go decoding, rejects multiple
roots, ambiguous POST endpoints and malformed KeyInfo, and does not resolve
external entities. Configured fingerprints remain SHA1 certificate fingerprints
as in the source; they do not replace mandatory SAML signature validation. An
empty allowlist is permitted for single ingress but rejected for multi ingress.
Expired certificates remain valid stored signing-key material, matching the
original fixture/test. Connected signed login and the full101 original Spar executions pass; finite
paired trust controls and durable switchback are recorded in the completion review.

The source's user_v2 key case-folds the unqualified subject and ignores NameID qualifiers and team. The Go mapping store preserves that key and UID-guarded deletion, including copy-on-lookup legacy migration. IdP purge must check Brig's team for every mapping before deleting accounts; issuer-wide deletion alone would cross a team boundary for shared version-2 issuers. Connected signed login now enforces request-to-issuer and assertion-replay checks. Original101, finite judge/security controls and actual102-action switchback pass. Existing read/check/write replay and UID-delete races remain source concerns.

Native certToCreds requires RSA/SHA256 certificate algorithm but imposes no RSA modulus minimum. Original SAML tests call mkSignCredsWithCert Nothing96 (RSA.generate's byte size:768 bits; the fixture exponent is17). Go1.26.9's VerifyPKCS1v15 enforces1024 bits by default, a demonstrated mismatch. Under the user's explicit instruction to mirror the current backend and record serious concerns, Spar alone uses //go:debug rsa1024min=0 to retain existing configured-key acceptance; this preserves the native policy rather than adding a minimum or changing Haskell/original fixtures. Every digest, signature, configured-key, issuer and replay check remains mandatory. It does not alter peer services or approve deployment/hardening. Small RSA keys are a serious inherited weakness: a reviewed future policy should require modern key sizes, with deliberate fixture/contract updates. The compatibility test proves default rejection, native-policy acceptance and rejection of a wrapped assertion even with a legacy key. Same-author implementation review is not an independent security audit.

The service-local signature adapter uses maintained goxmldsig1.6.1 canonicalizers,
standard Go RSA verification and standalone Decred RIPEMD160 v1.0.2. It trusts
stored configured keys only, ignores advertised response KeyInfo, rejects
ambiguous IDs and returns the verified assertion subtree. Native digest methods
SHA1/SHA256/SHA512/RIPEMD160 and RSA-SHA1/SHA256 remain supported, with exactly
one successful matching assertion-ID reference; failed additional references
are not universally fatal. Duplicate successful references and credentials are
rejected. Pinned certificates are not subject to web-PKI validity-date checks,
matching the native policy; the connected judge checks assertion timestamps.
ADFS whitespace repair is retried only after unmodified verification fails.
Actual native/Go comparison passes twelve signed cases plus nine paired tamper
controls, including an assertion with over1,000 nodes, without any session or
cookie. Native matching diagnostics establish the missing-request boundary for
accepted signatures. These relevant inputs remain byte-identical in final40; the completion review
links the separate current state/operations and final225 qualification.

Native form parsing enforces the pinned65336-byte URL/text parameter limits,
32-byte part names,10 files,32 part header lines and8190-byte header lines.
The streaming Go parser preserves the native generic500, InvalidRequest400 and
plain unsupported-content-type400 boundaries. Twenty actual native/Go probes
and no-authentication-effect/bounded-read regressions pass. Multipart per-part
limits count incoming chunks/read-ahead before delimiter detection; this is not
an atomic decoded-size guarantee. Native uploaded-file size is unlimited,
although count is bounded. Go streams/discards unused files without temporary
files or authentication effects; a new upload-size policy is separate work.
The attempted golang.org/x/crypto dependency was rejected by the unchanged
module vulnerability gate; the standalone ISC RIPEMD module passes that gate.
No scanner waiver, dependency fork or signature bypass was introduced.

Internal team cleanup follows native Spar.App.deleteTeam: delete tokens, then issuer-wide user_v2 mappings and IdP configuration for each team IdP. The native assumption that issuers are not shared between teams conflicts with version-2 shared-issuer configurations. Deleting one team may remove another team's SAML mappings for that issuer. This is a serious inherited cross-team state concern observed in source, not an exploit demonstrated by this increment. Preserve the native sequence; a future team-scoped key/policy fix requires reviewed contract work and switchback planning.

Email discovery is intentionally unauthenticated and feature-gated, exposing a matching SSO IdP code only for an SSO account with a Galley team and matching optional host. This can reveal membership/IdP association for known emails when enabled. Native diagnostics log queried emails and user IDs when multiple users are returned; Go keeps the native generic HTTP error without adding identity logs. No new rate-limit or privacy policy is introduced in this migration.

Native middleware accepts a gzip body with a missing trailer if its decoded JSON is complete (demonstrated by the real process comparison). Go preserves this behavior; JSON/XML and SAML signature validation remain mandatory. Do not treat gzip framing as authentication or transport integrity. A stricter compression policy would be a separate behavior change. Runtime GC gauges describe Go collections: gc_seconds_cpu is the mean CPU per collection in the sample interval, not a native last-cycle value.

Mobile URI compatibility: native uri-bytestring0.3.3.1 permits a raw Set-Cookie value in the redirect query and percent-encodes it on output; a cookie containing spaces in the path fails400bad-success-redirect. Preserve that distinction rather than net/url's automatic path escaping. Native creates the Brig session before rendering this failure, so an invalid redirect can leave an issued session without returning its cookie. This is inherited behavior; future redirect/session atomicity changes need separately reviewed policy/contract work. No production cookies or keys are used in the qualification fixture.
