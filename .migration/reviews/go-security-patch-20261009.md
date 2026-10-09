# Go security patch review

Same-author review by codex under the user's autonomous migration/self-review
instruction. Approved patch-only1.26.9 pins using official HTTPS release metadata,
four platform filenames/hashes. Linux amd64 SHA256
42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d
verified before extraction; actual compiler reports go1.26.9 linux/amd64.
Eight tooling boundary and32 migration tests pass. Nix unavailable/unrun;
CI already reads go/.go-version. Haskell dependencies/defaults unchanged.

GO-2026-6617 and related published HTTP2/TLS/parser defects failed the existing
module scanner; never bypassed. Federator module must additionally update x/net
0.60.0 and rerun final finite checks. Existing qualified1.26.8 peer images are
historical functional evidence, not current security assurance; rebuild/requalify
before production. No production or remote actions. Download receipt retained at
/home/operator/.cache/wire-migration/toolchains/go-security-20261009/receipt.json.
