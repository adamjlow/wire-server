Cassandra conversation and authenticated bot construction

Go1.26.9 full module format/tidy/verify/vet/race/unit/build/vulnerability/license
checks passed; go-check.log records actual checks and source-files.json identifies
tested inputs. No new dependencies, final clean executable or original E2E claim.
The report's module HEAD URL warning is not a compiler warning; the local module
license was classified AGPL and all dependency licenses were allowed.

Cassandra adapters preserve nullable metadata, active local members, parent-first
lookup with child priority, source pure-Cassandra remote membership, bound logged
member batches with reverse indexes and future deletion tombstone. A concrete
native-driver NULL-to-zero gap was fixed using custom nullable UnmarshalCQL before
MapScan. Tests verify absence versus present status0/empty text, legacy access
roles, epoch write times and malformed metadata. Source x1 is immediate; x5
has deterministic100/200/400/800/1600ms delay. Real DB effects still need original
job tests and switchback.

Bots use bound Cassandra lookup and registered RSA public-key fingerprints.
The existing public mock-service key's native fingerprint is
8a8cb719e22381046ca1b7f610a18edcef26abf1687c5c4746acb44f8111219f.
This value was independently generated with libcrypto.so.3 BN_bin2bn/BN_bn2mpi
on modulus/exponent extracted by openssl pkey -pubin -text -noout, then SHA256
of decimal RSA byte size followed by both MPI encodings. No private fixture was
printed or copied, and runtime code has no FFI/Haskell dependency.

Source Ssl.Util requires a matching native RSA pin plus CA verification or valid
self-signature and dates, without hostname checking. Go custom mandatory
VerifyConnection authenticates every TLS connection under that contract; its
InsecureSkipVerify flag replaces the standard hostname-only decision, not the
verification itself. Test-generated private keys exist only in memory. Real
loopback HTTPS tests verify source path/Bearer headers and callback survival after
attempt cancellation, while verifier tests reject wrong pins, expired or tampered
certificates.410/302 are terminal;503 has four total attempts; missing/disabled
services receive no requests. Callback errors do not affect ACK under the native
asynchronous best-effort contract. Process logging/drain still need startup work.

Inherited security concern: pinned bot authentication does not require hostname
matching. Compatibility concern: native RSA cipher list also includes finite-DHE
AEAD suites unsupported by Go TLS; providers offering only those suites may fail
negotiation. Current synthetic fixtures support shared TLS suites, but original
bot effects have not yet been tested. No exception or final parity claim is made.

Next: real migration-mode routing/locks/pending cleanup and consumer startup,
supervision/drain, followed by scheduled jobs/all seven migrations and unchanged
full final tests/operations/switchback/image/selector. Protected schemas/original
assertions are unchanged. Native local Haskell checks unavailable/unrun.
