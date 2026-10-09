# Optional Go security patch, 2026-10-09

Federator's required module scan reports new HTTP/TLS vulnerabilities in Go1.26.8
and x/net0.59.0. Fix with Go1.26.9 and candidate-local x/net0.60.0; never bypass
module scanning. Official https://go.dev/dl/?mode=json supplies archive identities;
https://pkg.go.dev/vuln/GO-2026-6617 identifies the patch. This framework task owns
only optional toolchain pins/README and its review; candidate module changes remain
in the requeued federator implementation. No Haskell/Nix dependency/default edits.

Fetch metadata using verified HTTPS, pin four supported platform filenames/hashes,
download only official Linux amd64 archive and verify SHA256 before extraction to
task-owned cache. Keep old compiler for historical receipt validity. Check version,
8 tooling boundary tests and32 migration tests. Nix unavailable/unrun. Security
patch is a justified finite prerequisite; original77 must run on patched final
federator binary. Existing4 Go binaries qualified with1.26.8 retain historical
functional evidence but need patched rebuild/requalification before production;
no earlier security receipts are silently rewritten. Haskell stays default.
