# Offline comparison foundation

`compare.py` compares independently captured synthetic observations without any
network requests or shell execution. This is a foundation for the live runner;
it neither launches services nor proves that supplied captures came from them.
Every report says `parity_verified: false`, even when comparison succeeds.

Run `python3 -m unittest discover -s tools/parity -p 'test_*.py' -v` for self-tests.
Use `python3 tools/parity/compare.py manifest.json reference.json candidate.json report.json`
to produce a new report. Exit 0 means complete trace agreement, 1 means comparison
failed/blocked/skipped, and 2 means invalid input or an unusable report destination.
Existing reports are never overwritten. Inputs are bounded to 32 MiB and inline
bodies to 8 MiB. Larger streaming captures require a subsequent artifact protocol.

The tests provide schema examples. The manifest pins upstream/contract/fixture
revisions, the comparator's SHA256, both source/image/config identities, declared
provenance receipts, dependency images, clock/seed, isolated literal loopback
endpoints and required scenarios/steps/assertions. Captures identify their role,
language, source/image/config/oracle identity and each observation's result.
An unverified provenance declaration blocks every case. Declarations and passing
assertions in a supplied file still require independent verification.

Steps record the identical logical request (including explicit caller headers),
HTTP response, ordered side effects and required assertion outcomes. All response
headers are compared, with case-insensitive names and ordered repeated values.
Bytes are exact. Optional `json-object-order` permits only object member ordering;
numeric spelling, null/missing, bool/number, array order and duplicate members
retain meaning. Content lengths must match captured bytes except HEAD/204/304.
There are no configurable exclusions or broad body/header/URL normalizations.

Reports contain mismatch categories and input hashes, not capture bodies/header
values. Only synthetic input is permitted; the declaration is not an automatic
secret detector. Do not use production captures. Store captures separately with
the same review/privacy rules; no raw observations are published by this CLI.

Still required for the complete goal: authenticated live capture/image provenance,
service-specific complete fixtures, transport/gzip comparison decisions, validated
cargohold key/token/expiry/signature mapping, S3/federation effects and cross-runtime
storage switchback, operational thresholds, deployment selection and Haskell
build/default checks. This comparator and its examples cannot satisfy those gates.
