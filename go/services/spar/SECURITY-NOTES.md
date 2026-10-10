# Inherited Spar concerns

This candidate remains under construction. Preserve reference behavior and record
concerns; no Haskell repair or security-policy change is part of this migration.
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
exploit; the complete authentication effects still need qualification. The Go
construction follows that source sequence. Do not silently add an ownership
policy or infer approval for a security exception from parity/self-review.

XML metadata parsing uses namespace-aware standard Go decoding, rejects multiple
roots, ambiguous POST endpoints and malformed KeyInfo, and does not resolve
external entities. Configured fingerprints remain SHA1 certificate fingerprints
as in the source; they do not replace mandatory SAML signature validation. An
empty allowlist is permitted for single ingress but rejected for multi ingress.
Expired certificates remain valid stored signing-key material, matching the
original fixture/test. Connected signed login passes two unchanged original
scenarios; whole-service trust qualification remains pending.

The source's user_v2 key case-folds the unqualified subject and ignores NameID qualifiers and team. The Go mapping store preserves that key and UID-guarded deletion, including copy-on-lookup legacy migration. IdP purge must check Brig's team for every mapping before deleting accounts; issuer-wide deletion alone would cross a team boundary for shared version-2 issuers. Connected signed login now enforces request-to-issuer and assertion-replay checks. Two original login scenarios and finite judge/security tests pass; the final whole-service security/state matrix remains pending. Existing read/check/write replay and UID-delete races remain source concerns for the final review.

Native certToCreds requires RSA/SHA256 certificate algorithm but imposes no RSA modulus minimum. Original SAML tests call mkSignCredsWithCert Nothing96 (RSA.generate's byte size:768 bits; the fixture exponent is17). Go1.26.9's VerifyPKCS1v15 enforces1024 bits by default, a demonstrated mismatch. Under the user's explicit instruction to mirror the current backend and record serious concerns, Spar alone uses //go:debug rsa1024min=0 to retain existing configured-key acceptance; this preserves the native policy rather than adding a minimum or changing Haskell/original fixtures. Every digest, signature, configured-key, issuer and replay check remains mandatory. It does not alter peer services or approve deployment/hardening. Small RSA keys are a serious inherited weakness: a reviewed future policy should require modern key sizes, with deliberate fixture/contract updates. The compatibility test proves default rejection, native-policy acceptance and rejection of a wrapped assertion even with a legacy key. Same-author implementation review is not an independent security audit.

The current goxmldsig1.6.1 adapter verifies stored configured keys only, ignores advertised response KeyInfo, restricts RSA signature methods to native SHA1/SHA256, rejects ambiguous IDs and returns only the verified referenced subtree. Its copy of the configured certificate removes the SDK's web-PKI date constraint because native expired pinned keys are supported; the connected judge enforces assertion timestamps with actual clock checks. ADFS whitespace repair is retried only after unmodified verification fails, matching source allVerifies. These crypto tests are not complete login qualification. Remaining concrete SDK differences include native RIPEMD160 digest support and the SDK's namespace traversal limit; resolve these before whole-service verification, without patching originals or declaring unsupported forms compatible.

Internal team cleanup follows native Spar.App.deleteTeam: delete tokens, then issuer-wide user_v2 mappings and IdP configuration for each team IdP. The native assumption that issuers are not shared between teams conflicts with version-2 shared-issuer configurations. Deleting one team may remove another team's SAML mappings for that issuer. This is a serious inherited cross-team state concern observed in source, not an exploit demonstrated by this increment. Preserve the native sequence; a future team-scoped key/policy fix requires reviewed contract work and switchback planning.

Email discovery is intentionally unauthenticated and feature-gated, exposing a matching SSO IdP code only for an SSO account with a Galley team and matching optional host. This can reveal membership/IdP association for known emails when enabled. Native diagnostics log queried emails and user IDs when multiple users are returned; Go keeps the native generic HTTP error without adding identity logs. No new rate-limit or privacy policy is introduced in this migration.

Native middleware accepts a gzip body with a missing trailer if its decoded JSON is complete (demonstrated by the real process comparison). Go preserves this behavior; JSON/XML and SAML signature validation remain mandatory. Do not treat gzip framing as authentication or transport integrity. A stricter compression policy would be a separate behavior change. Runtime GC gauges describe Go collections: gc_seconds_cpu is the mean CPU per collection in the sample interval, not a native last-cycle value.
