# Proxy inventory task plan

Task: `proxy-inventory`. Scope: discover current boundaries only. No runtime edits,
Go code, contract approval, integration execution or deployment. Pin `git rev-parse
HEAD` in the inventory when the task is actually claimed; no baseline is fabricated.

1. Read root policy and existing proxy source, Cabal/Nix config and README.
   Trace `services/proxy/src/Proxy/API/{Public,Internal}.hs`, `Proxy/Proxy.hs`,
   options, environment and run modules into shared library definitions.
2. Inspect third-party interactions, config examples, chart/nginz routing and
   existing test locations. Read test code without running integration. Record
   exact source symbols and revision for observed behaviour; do not infer unused
   endpoints from filenames alone.
3. Create `docs/go-migration/services/proxy/inventory.md` using the inventory
   template. Enumerate interfaces, dependencies/effects, validation/security,
   config/operations, unit/golden coverage and unknowns. Use synthetic data and
   redact credentials; no external API calls are needed.
4. Link existing tests and permitted commands. Record missing coverage and propose
   bounded follow-ups in the inventory; adding tasks/service gates is coordinator
   work outside this claim. Add the claimed changelog entry.
5. Run `make migration-check migration-test`, attach the inventory and review
   artifacts at the inspected revision, then hand off for independent review.

Acceptance: every inventory section has source evidence or an explicit unknown;
all proxy entry points found during inspection are mapped; checks pass. Review
must verify source citations and that nothing asserts unexecuted parity. Rollback
is reverting these documentation/task changes; service state remains registered
until the coordinator separately records the completed inventory gate.
