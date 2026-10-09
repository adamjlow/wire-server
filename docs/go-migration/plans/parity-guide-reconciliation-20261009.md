# Reconcile legacy parity guide and compiler guard

User requests reconciled docs before background-worker goal. Source audit found
legacy proxy harness requiring Go1.26.8 and guide claiming original environment
unavailable. Align only its compiler guard to existing central go/.go-version,
update guide with current original77/artifact/coverage and private-environment
receipts, and preserve standalone comparator limitations/historical captures.
No reference assertions, comparator rules, defaults, service logic or permissions
change. Continued delegated same-author review; no production/push action.

Scope guard/guide/existing Python test only. Reuse existing failure-does-not-pass
regression at current pin; verify exact current version accepted and old version
rejected before Docker/artifacts. Run focused test_harness suite, link/diff/scope
checks; actual patched original77/service quality remains unchanged and reused.
No expensive live API or e2e rerun justified by this compiler guard correction.
Then finish docs and create whole background-worker goal from its ready plan.
