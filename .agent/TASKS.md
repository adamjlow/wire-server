# Taking and handing off work

1. Read root/local policy, the service record, dossier and plan. Inspect git
   status. Run `make migration-check` and select a `queued` task whose dependencies
   are `done`. `blocked` tasks require their documented blocker to be resolved.
2. A coordinator assigns a unique owner, branch/worktree and changes the task to
   `active`. Create task branches from the fork's `go-migration` branch and target
   PRs there; `develop` is reserved for upstream tracking. Merge the claim into
   `go-migration`, the shared coordination branch, before dispatch.
   Rebase and revalidate claims; two branches independently passing are not a lock.
3. Read anywhere. Write only the claimed exact files/directory prefixes, including
   your task record and changelog. Paths are literal; no wildcards. Each active
   or `review` task reserves all its write paths. Review keeps the reservation.
   Divide service code and tests into explicit disjoint files/directories when
   parallelizing. A shared library, manifest, CI file or service record gets one
   serialized task owner. Never use another agent's uncommitted edits as evidence.
4. Implement and collect evidence against the declared criteria. Document findings
   in the dossier and mark `review` when ready. Run
   `python3 tools/migration/check.py --base <base-sha> --task <task-id>` to check
   changed tracked and untracked files against scope before handoff. Run this in
   a clean task worktree; base is the pre-task coordination revision. This check
   includes staged and unstaged changes, deletions, and both sides of renames.
5. Reviewers verify evidence, security, oracle independence, and scope. Move to
   `done` only when every criterion has passing evidence and independent review
   is recorded. Contract-change and operations reviews must be by an authorized
   human. The coordinator alone updates service gates in a separately scoped task.

Allowed transitions: `queued → active → review → done`; `active/review → blocked`;
`blocked → queued`; `review → active` for rework. Terminal `done` records are
immutable. Create a follow-up for corrections. When upstream/reference/candidate
changes invalidate evidence, a scoped coordination task clears the service's
`compatibility` claim; a new verification task restores it for tested revisions.
Keep historical gates intact; follow [upstream sync](../docs/go-migration/UPSTREAM.md).
An active task may be marked done in its completion PR after independent review;
the transient review state need not require a separate merged bookkeeping PR.
Claimed identity, scope, dependencies, plan path and criteria cannot change:
block and requeue the task through coordination before changing them.

Blocked tasks release their reservation. Record findings, remaining work and a
clear blocker in `handoff`; return via the claim process before writing again.
Do not steal abandoned work by timeout. The coordinator confirms the previous
writer has stopped, transitions to blocked, then requeues/reassigns it.

Task files are the queue; git provides history. Evidence records name criterion,
command (or review procedure), pass/fail/blocked result, exact tested revision,
and a durable artifact URL or repository path. Never store secrets in these fields.
Commands are descriptions, never automatically executed by the validator.
