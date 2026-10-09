# Legacy parity guide and compiler guard review

Approved by codex, same-author review explicitly delegated by user,2026-10-09.
Actual stale Go1.26.8 guard now reads the existing central go/.go-version, currently
1.26.9; no automatic upgrade or weaker version check. Existing failed-capture
regression uses that pin and still cannot write pass evidence after failure. New
negative proves old compiler rejected before Docker or artifact creation.
All9 focused harness tests pass;7 guide links resolve; scope/diff checks pass.

Guide now links actual original77/image receipts and owned Docker environment,
preserves offline/preflight limitations and explains that background-worker jobs
require their own source-derived acceptance. Source captures/assertions/comparator
normalization, service logic, defaults and permissions are unchanged. Existing
patched service quality/original77 evidence is reused, not rerun for this guard
correction; no additional live/e2e test claim. Native Haskell checks unrun because
Haskell code unchanged. Ready to create requested whole background-worker goal.
