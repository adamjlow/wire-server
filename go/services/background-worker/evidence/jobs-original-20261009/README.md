Original notification and queue-lifecycle rerun:40/40 pass

Exact static Go1.26.9/CGO0 binary from clean source
00557161c5218fba7ce290ba57536561298a35e3, SHA256
80b1f77974affea10b17d55a795c95126f9e2992d9848280c6af6569c01b7f92.
Only background-worker is Go; every other staged service is Haskell. Pinned runner
cd03212430eef6cfde0324b50885c12980ee211483ab27dde6ee730252966cd2,
runner revision344c9e4cbda09252c1530e4fbffe38c2000f9efd.

Original unchanged gundeck40:40 distinct XML cases,40 pass, no failures/errors/
skips. Inner and controller receipts both report actual execution/pass and process
exit0. The two prior failing cases now pass: backend pusher queue-deletion recovery
and temporary queue cleanup. Source decimal version header and actual background
job consumer/queue startup corrected their concrete causes. Prior38/40 failure
receipts remain unchanged in watcher-20261009.

Owned cleanup exit0; post-cleanup Docker label queries returned no containers,
networks or volumes for projectwire-e2e-7e4eb9c752a94cbc927f44ab37722dfa.
Original-result.json retains all cases, exact identities and raw cache log hashes.
During the run, the actual worker endpoint8089 readiness returned200/empty for all
three Rabbit concerns; this is not a final bounded load/shutdown operations smoke.

This proves only the selected original notification/DLQ/queue-lifecycle profile.
Original worker47 and actual group business/storage paths remain untested. No
whole-worker/scheduled/migration or six-Go qualification is implied. Later scheduled
storage/worker sources are not in this exact binary. All remaining contract,
operations and durable switchback requirements still apply; Haskell remains default.
