# MLS signed upload and seven original compiled tests

Qualified source `c061bba2170cc3c178245ee2e3c02086f994726b`, Brig SHA256
`c92313e35aa77661344bd3f074d382e9669b8060c0adb22d8513e2911eb391f8`.
Pinned Go format/dependency/vet/unit/race/security/license/build and static
normal-unit checks pass. No new dependency or host dependency installation.

Production upload preserves raw package and leaf TBS slices, performs labelled
Ed25519/P256/P384/P521 signature verification with standard Go crypto, then
protocol/leaf signature/credential identity and public-key/source/capability,
exact lifetime and local stored-key validation. All packages validate before a
logged LocalQuorum/retry5 batch. Package reference hashes use native TLS
RefHashInput encoding and suite hash. Replace validates its announced suite set,
deletes selected suites with bounded/joined operations, then validates/uploads.
That destructive failure ordering is deliberate native behavior, not rollback.

Seven unchanged compiled Haskell cases pass against dynamic Go Brig with Go
Gundeck/native remainder: DeleteKeyPackages, UploadNoKey, UnsupportedCiphersuite,
and Count on suites0001/0002/0005/0007. These use real mls-test-cli packages,
stored client keys, signed leaf/package material and package refs. Strict XML
has exactly seven expected names and no failures/errors/skips; XML SHA256
`e869305b253fe0b1be08842b905be995fae70b47b22cbd48a5fb982681baecf0`.
Original integration SHA256
`56f56a8e0f8ffd24a11982f13385d41f4d92e238e6900bcbca1ddf7d21825f4a`.
These are reused tests, not a Go rewrite of assertions.

Prior four deletion/five count/ten MLS/14 public clients/13 federation/five
single/five multi/seven lifecycle/17 paired comparisons and actual client/
prekey/MLS native-Go-native stored-byte switchback pass. All25 managed exits0,
runner0, cleanup0, independent owned namespace audit empty, no force. The
original runner's queue teardown notes missing federation credentials; owned
Docker resources were removed and audited, not described as external cleanup.

User-delegated same-author review accepts this finite scoped evidence. Whole
Brig remains incomplete and no gate advances. Claim and original replace cases
(which also claim packages), actual durable signed-package upload/replace
switchback, native destructive replace failure and X509/DER/identity/Base64/
malformed-error-detail variants remain unqualified. In particular Basic History
credentials need native identity-mismatch error alignment; X509 parse errors are
currently generic rather than native exact diagnostics. Native X509 parses the
first certificate without certificate-chain trust/signature validation; preserve
and document this inherited concern, do not silently harden migration acceptance.
Other families/final535/eight-Go/all-entity3mode/operations/image/default-selector/
whole review remain required. Haskell supported/default; new commits local only.
