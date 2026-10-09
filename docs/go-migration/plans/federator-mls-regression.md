# Original MLS interoperability regression

This repair is a dependency of the active whole-service background-worker goal,
not a replacement goal. Its protected contract task is waiting for this correction.
The Haskell service/defaults/schemas and original assertions stay unchanged.

Observed: original Migration.Conversation.testMigrationToPostgresMLS passed with
all Haskell peers, failed HTTP533 (expected201) with the five identified patched
Go peers in two full worker runs, and failed alone on the new strict targeted
profile. Failures occur while creating MLS conversations before data migration.
Actual receipts/XML/cleanup0 are in the background-worker oracle. No attribution
has yet been proved; federation RPC/transport is the primary suspect. Temporarily
clear federator compatibility, preserve historical gates and prior evidence.

1. Reuse original single-case `background-worker-mls` profile and the current
   exact patched peer tuple. Obtain the actual failing RPC/transport response,
   establish whether federator is responsible, add a focused reproduction and fix
   the underlying behavior. Bounded secret-free diagnostic logging is allowed;
   no payload/certificate/private-key/connection-string logs. Never mask errors.
   If another component is responsible, stop/rescope rather than changing it here.
2. Compile an early corrected executable using Go1.26.9, GOTOOLCHAIN=local,
   GOWORK=off. Keep dependency pins, verified TLS, routing/auth/header/body/error
   contracts, context cancellation and strict input/resource boundaries intact.
   Use parameterized SQL where relevant; do not invent retries that duplicate
   requests or silently broaden trust/policy. Source ambiguity requires direct
   Haskell comparison, no security/contract exception is authorized.
3. Against the exact executable run required `python3 go/check.py federator`,
   frozen35 unchanged, original federator5, isolated original MLS and corrected
   whole worker47 with four unchanged patched Go peers. Reuse earlier unchanged
   service checks; native GHC/Cabal/Nix remain unavailable/unrun. All tests keep
   strict no-failure/error/skip/missing-case validation and process/cleanup checks.
4. Build the optional nonroot image, prove matching executable/compiler/CA layout,
   health and SIGTERM on owned read-only/private fixtures. Record exact tested
   revision and artifact tuple, retain failed evidence. Same-author review is
   delegated by the continuing user instruction; annotate it honestly. Publish
   compatibility separately only after every finite requirement passes, then
   resume worker contract and early runnable Go implementation.

Use existing task-owned synthetic Docker environment only: all declared networks
internal, no host ports, source/fixture mounts read-only, owned volumes/cleanup.
One stack at a time because fixture subnets are fixed. No remote push/production.
Build commands retain the mandated output filter and actual build exit status.
Framework/scope commands use full real SHAs; metadata alone needs no service rerun.
Evidence: go/services/federator/evidence/mls-regression-20261009 and migration review.

## Executed diagnosis checkpoint (2026-10-09)

Static diagnostic federator c18972ece9edfd80e0c4f1d2fa12b9f210ea1d70 passed
original isolated MLS with all other services Haskell (33.18s), but failed with
all five Go peers (12.74s). Bounded metadata located a get-mls-clients transport
failure to d1.example.com; the public 521 label is generic and does not prove
TCP refusal. Other adjacent RPCs returned 200. No attribution or behavioral fix
is established. Original assertions, schemas and TLS verification are unchanged.

The categorized diagnostic binary built from
75c2e8e508ba27cd51aecdb21d6ce50948276fd9 passed the same original single case with
all five Go peers, process0/cleanup0. This is an intermittent failure, not a
correction. The full worker47 baseline and compatibility restoration remain
unqualified. Diagnostics must be removed before final source/image qualification.
A bounded local 800-request concurrent single-use HTTP2 probe passed; it did not
reproduce the fault and its temporary test was removed. Do not add generic retries.

The earlier CGO-enabled diagnostic failed to launch in the Nix container due to
its absent ELF interpreter; no E2E ran. Its failed receipt is retained alongside
the correctly static builds. Evidence is in the claimed module evidence directory:
original strict XML/run/environment, bounded trace and actual source/build identity.
Native Haskell builds remain unavailable/unrun; no worker implementation is claimed.

The one bounded repeat of the same categorized static executable also passed
original MLS with five Go peers, process0/cleanup0. Exact same binary hash is in
both original run receipts. No new transport error was captured; attribution
remains unresolved. The current diagnostic source passes `go test ./...` with
Go1.26.9/GOTOOLCHAIN=local/GOWORK=off/CGO_ENABLED=0. Source remains diagnostic only;
no final image, whole47 clean baseline, corrected compatibility or worker build
is claimed. Stop broad repetition: the next diagnostic step must isolate the
observed transport path or reference/environment cause with concrete evidence.
