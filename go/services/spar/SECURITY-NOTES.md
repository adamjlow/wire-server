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
original fixture/test; signed login validation is still pending.

The source's user_v2 key case-folds the unqualified subject and ignores NameID qualifiers and team. The Go mapping store preserves that key and UID-guarded deletion, including copy-on-lookup legacy migration. IdP purge must check Brig's team for every mapping before deleting accounts; issuer-wide deletion alone would cross a team boundary for shared version-2 issuers. Request-to-issuer and assertion-replay tables are implemented, but signed login has not yet been wired to enforce them; their presence does not qualify login security. Existing read/check/write replay and UID-delete races remain source concerns for the final review.
