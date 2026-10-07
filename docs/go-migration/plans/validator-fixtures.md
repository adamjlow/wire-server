# Independent validator test fixtures

Task: `validator-fixtures`. Claiming proxy inventory changed its live status to
active, causing ten validator-test failures because test setup loaded the mutable
work queue. Tests must remain deterministic as real migration work proceeds.

Replace setup's live service/task reads with fixed synthetic version-1 records in
the temporary repository. Keep all acceptance assertions and validation rules
unchanged. Use synthetic identities, local artifact files and no network or service
execution. Write scope: this plan, task record, `tools/migration/test_check.py` and
the relevant changelog entry. No contract or runtime source changes.

Validation: `make migration-check migration-test`; all 32 tests must pass with the
live proxy task claimed or blocked. Record the result and tested code revision for
independent review; do not claim self-review as completion.
