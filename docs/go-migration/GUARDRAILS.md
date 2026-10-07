# Guardrails and review boundaries

## Autonomous work

Agents may inspect broadly, claim bounded tasks through the coordinator, implement
within approved contracts, run permitted local checks, and record discoveries.
Blocked checks stay blocked. Write permissions are literal repository-relative
files or directory prefixes, not globs. Concurrent active/review scopes must not
overlap, including shared docs and task/service records.

## Three human controls

1. **Contract/oracle changes:** an authorized maintainer approves a separate
   `contract-change` task and review before candidate work consumes it. This
   includes fixtures, goldens, normalization, compatibility expectations and
   reference behaviour. Record old/new behaviour, justification, impacted clients
   and negative tests. An unsafe reference behaviour is escalated here.
2. **Per-deployment production switches:** an authorized operator approves exact
   service/image/config/environment, rollout, abort thresholds and rollback window
   in an `operations` task. Merely reaching `available` authorizes no deployment,
   changes no default and grants no permission to remove Haskell support.
3. **Guardrail changes:** maintainers review policy, validator, workflow and
   lifecycle changes independently of tasks those changes could unblock.

Preserve CODEOWNERS. The fork owner must configure branch protection, required
`Migration framework / validate` checks and appropriate authorized reviewers for
the fork; upstream team names alone do not establish enforceable ownership here.
No account settings are changed by this patch. Do not grant agents permission to
approve their own contract exceptions or production changes.

## What checks enforce

`check.py` rejects unknown fields/states, malformed paths/revisions, unresolved or
cyclic dependencies, concurrent ownership conflicts, missing passing evidence,
and service gates without completed tasks of the required kind. Implementation
scopes cannot overlap reference, shared integration/fixture or declared oracle
paths. With `--base`, it rejects skipped service stages, illegal task transitions,
removed records and edits to completed tasks; existing oracle paths cannot be
removed through an ordinary state update. With `--task`, it checks the actual diff
against that task's claimed write paths. CI runs state and transition validation;
per-task diff validation is a handoff requirement because a PR may contain
several serialized tasks. The Haskell reference directory is required at every
stage. Current compatibility, when populated, must pin upstream/reference/candidate
revisions and a completed verification task with evidence for that candidate.
Changing those pins requires fresh verification rather than relabelling old evidence.

These are consistency checks, not a security sandbox or review authenticator.
They cannot determine whether evidence is true, a URL is durable, a reviewer is
human/authorized, or an inventory has found every implicit contract. Review must
check exact tested revisions/artifacts, approvals, meaningful assertions and
newly discovered oracle paths. Treat untrusted records as data; the tool never
executes commands or fetches URLs. Do not put secrets or real customer data in
records, fixtures, logs or review links.

Protect the validator and workflow through branch rules: a PR can otherwise
modify its own checks. Stale evidence after a contract/reference/candidate change
must be rerun; documentary gates do not automatically prove the latest binary.
Upstream synchronization must follow [UPSTREAM.md](UPSTREAM.md): preserve upstream
behaviour, record impacts, clear affected compatibility claims and revalidate.
The validator does not fetch upstream, infer whether every commit affects a
service, or run Haskell/default-deployment tests. Those checks need real evidence.
