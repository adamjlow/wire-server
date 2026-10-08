# Agent operating rules

## Mission and boundaries

Maintain an upstream-compatible Wire server with optional Go implementations of
whole services. Haskell remains supported, the default, and the behavioural
reference even when a deployment chooses Go. Preserve existing build, install,
configuration and upgrade behaviour for users who do not opt in. Behavioural
compatibility matters more than implementation similarity: do not mechanically
translate Haskell abstractions into Go.

- `AGENTS.md` is policy; `docs/go-migration/` is persistent knowledge;
  `.migration/` is current machine-readable state; CI/tests supply evidence.
- Significant migration work needs an executable plan following
  [.agent/PLANS.md](.agent/PLANS.md). Every task needs bounded write scope,
  acceptance criteria and required evidence; follow [.agent/TASKS.md](.agent/TASKS.md).
- Read broadly, write only within the claimed task's paths. Check git status
  before work; preserve unrelated changes. Parallel writers need non-overlapping
  claims and separate worktrees. Shared files need a serialized owner.
- Implementation tasks must not weaken their oracle to pass. Contracts, golden
  fixtures, normalization rules, compatibility expectations and reference
  behaviour change only in explicit, separately reviewed `contract-change` tasks.
- Advance service state only when the documented objective gates pass. Missing,
  skipped or unavailable checks are not passes. Never invent evidence or approvals.
- Haskell and Go coexist as separate service processes. Do not split a service
  across a shared Haskell/Go runtime. Do not bundle language replacement with
  unrelated architecture, storage or protocol changes unless explicitly planned.
- Operators select implementations per service. Never infer a new default or
  permission to remove Haskell from Go availability or a deployment's rollout.
  Preserve compatible data and a tested route back to Haskell.
- Keep Go work additive and upstream edits small. Do not reorganize upstream
  code, require Go for existing Haskell workflows or fork shared contracts to
  accommodate a candidate. Follow [upstream sync](docs/go-migration/UPSTREAM.md);
  reassess compatibility on upstream changes and record exact tested revisions.
- Keep fork work on `go-migration`; task PRs target that branch in `origin`.
  Reserve `develop` for tracking upstream. Review upstream merges into
  `go-migration` on separate sync branches; never force-push shared history.
- Do not silently change externally observable behaviour. Record new behaviour,
  constraints and unresolved questions in the service dossier, not just chat.
- Humans control contract exceptions, per-deployment production switches and changes
  to these guardrails. Ordinary bounded work proceeds autonomously within policy.
  See [guardrails](docs/go-migration/GUARDRAILS.md) and
  [lifecycle](docs/go-migration/SERVICE-LIFECYCLE.md).

## Working efficiently

- Prioritize runnable service code and direct comparisons with the Haskell
  reference. Establish a small executable slice early, then expand coverage.
  Add migration infrastructure only to solve a concrete implementation or
  validation problem encountered in that work.
- Reuse the existing service plan, reference captures, tooling and recorded
  decisions while their inputs remain valid. Update the relevant document with
  new findings; do not create another discovery or approval task for an already
  settled question. Keep current status and evidence in the task record.
- Choose tasks around meaningful outcomes. Include routine planning, code,
  tests, documentation and review evidence in the same bounded task where its
  kind and scope permit. Separate genuine contract/oracle changes, deployment
  operations and concurrent ownership; preserve the validator's protections.
- A single writer may work directly on `go-migration`, with that actual branch
  recorded in the claim. Extra worktrees and centrally merged claims are needed
  for concurrent writers, not routine solo increments. Never work on `develop`.
- Run focused checks during development and required checks at handoff. Reuse
  successful evidence for unchanged inputs; rerun checks when relevant code,
  dependencies, fixtures, tooling or environment changes, or failures remain.
  Bookkeeping alone does not justify repeating expensive service builds.
- Keep metadata commits to the minimum needed for legal claim transitions and
  exact tested-revision evidence. Do not chase a self-referential final SHA or
  create separate tasks for each planning/review/status update.
- Agents own implementation, test execution, evidence review and ordinary bounded
  completion. The user is not a manual test runner. Reuse original public e2e
  assertions against selected binaries; copied request specifications alone are
  not execution of those tests. Establish dependency/fixture setup before service
  migration and fail clearly when required setup or cases are absent.
- Define a finite service acceptance matrix before implementation: public API
  and effects, security negatives, relevant failure behavior, one operational
  smoke and switchback. Add tests for concrete uncovered risks or regressions;
  stop when that matrix passes. Do not pursue exhaustive incidental/runtime
  identity or add another gate merely because more tests are possible.
- Use the automated [harness](tools/parity/README.md) and existing upstream CI.
  Rerun expensive checks only when their relevant inputs change. Unchanged
  Haskell code retains its existing CI; unavailable local full-repository builds
  are recorded, never converted to a pass or used to repeat all Go evidence.
- Honor explicit user review delegation within its stated scope. Record the
  authority once and cite it; label same-author review honestly and never imply
  independence. Delegation does not replace tests or approve security exceptions,
  production switches or an unverified compatibility claim.
- If preparation produces no runnable increment or new behavioral evidence
  after 15 minutes, reassess the next step and report the concrete blocker.
  Subsequent updates should identify implemented behavior, test findings or a
  necessary unresolved dependency. Repeated documentation-only progress is a
  reason to narrow the work and return to implementation or direct validation.

## Existing Haskell project

Services live in `services/`, libraries in `libs/`, and the current integration
suite in `integration/`. Haskell work uses Polysemy effects and shallow services;
legacy business logic is still being moved into libraries. This is a Haskell
convention, not a requirement to reproduce its architecture in Go. Prefer record
dot syntax over lenses for simple cases. Follow the existing
[coding conventions](docs/src/developer/developer/coding-conventions.md).

# Development Environment

The development environment is based on Nix flakes. Entrypoint is the
`flake.nix` file. More Nix related code is in `nix/`.

## Local

All tools and dependencies are provided by the `direnv` environment. I.e. if
you're on a local machine (e.g. developer laptop) and the current working
directory is inside this project directory, then all required tools and
dependencies are automatically available in the environment.

Local agents (e.g. copilot-cli) will probably want to use this way.

## Cloud / non-direnv

In cloud environments or when `direnv` is not installed, `nix develop` starts a
shell with all required tools and dependencies.

To directly execute commands in this environment, run `nix develop
--command bash -c "<COMMAND>"`.

Remote agents will probably want to use this way.

# Commands for Tasks

- Create a `cabal.project.local` with optimization turned off to get faster
  feedback (improved compile times): `make cabal.project.local`
- Align Cabal and Nix dependencies: `make regen-local-nix-derivations`
- Run formatter: `make format`
- Run linter: `make lint-all`
- Compile all Haskell packages (type check and see warnings):
  `make c | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Compile a specific Haskell package (type check and see warnings):
  `make c package=<PACKAGE> | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Compile all Haskell packages and run all unit tests:
  `make c test=1 | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Load all Haskell packages in GHCI for faster feedback:
  `cabal repl all --enable-tests --enable-multi-repl`
  - Reload inside GHCI (faster than starting a new session): `:reload`
- Load a single Haskell package in GHCI for faster feedback:
  `cabal repl <PACKAGE> --enable-tests`
  - Reload inside GHCI (faster than starting a new session): `:reload`
- Run tests for a single package (alternatives):
  - `make c package=<PACKAGE> test=1 | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
  - `cabal test <TESTSUITE> | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`

## Quality expectations

Haskell changes must type check without warnings and all changed packages' unit
tests must pass. Before finishing, check the whole project, all unit tests,
Cabal/Nix dependency alignment and formatting using the commands above. Preserve
`-Werror`. Record exact failures or unavailable tools; never claim unrun checks
passed. Dependency changes require regenerating and reviewing local Nix derivations.

Do not run integration tests (`make ci`, `make ci-fast`, `make ci-safe`,
`make devtest` without a unit-test package, integration executables or test suites).
They require specific environments. Future parity integration runs require an
explicit maintainer-authorized environment and policy change first.

The user's 2026-10-07 explicit self-review delegation is recorded in
[the bounded preparation decision](.migration/reviews/2026-10-07-self-review.md).
For the listed preparation tasks, review may be by the same actor with a clearly
annotated delegated self-review label; do not claim an independent reviewer.
This scoped exception overrides the generic independent/human-review requirement
for those tasks only. Every substantive evidence criterion still applies.
The isolated reference characterization environment in that decision and
[plan](docs/go-migration/plans/delegated-review.md) is authorized. This permits
only the new task-owned synthetic runner with internal Docker networking,
trusted test TLS and loopback client ports. Existing integration suites remain
prohibited. No production change, security exception, or candidate compatibility
claim is authorized by preparation review.

Build commands must end with
`| grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`.
Use Bash `set -o pipefail` and inspect the build exit status: grep can exit 1 if
all output is filtered, and must not mask a failed build.
For fast feedback use `cabal repl <PACKAGE> --enable-tests` or
`cabal repl all --enable-tests --enable-multi-repl`, then `:reload`.
Framework validation: `make migration-check migration-test`.

Preserve [PR guidelines](docs/src/developer/developer/pr-guidelines.md), the PR
checklist and CODEOWNERS. Add an appropriate `changelog.d/` entry; retain schema,
API routing and configuration review requirements. Do not edit generated files
by hand or bypass the Nix/Cabal workflow.

## Security requirements

For each bounded coding task, state concrete security requirements and relevant
framework/version constraints in its plan before implementation. Explain security
choices and alternatives. Refine underspecified security work into scoped tasks.

- Use synthetic data/placeholders; never commit credentials, production secrets,
  customer-identifying data or unsanitized captures. Follow [SECURITY.md](SECURITY.md).
- Treat input as untrusted: validate server-side, bound resource use, use
  parameterized queries and framework output escaping where applicable. Errors
  must avoid sensitive details while preserving the reviewed external contract.
- Keep TLS verification enabled. Do not use eval or execute untrusted task text.
- Prefer established, maintained dependencies. Check selected versions for known
  high-severity vulnerabilities; do not add dependencies without justification.
- Test security boundaries and malformed input, integrate those tests into CI,
  and explain the protections. Never weaken auth, crypto or security tests to
  obtain parity; resolve unsafe reference behaviour through reviewed contract work.

## Containerized e2e preparation

The user's 2026-10-08 Docker setup request authorizes building and inspecting the
pinned task-owned runner in [its plan](docs/go-migration/plans/e2e-container.md).
Read-only original-suite `--list` discovery inside that non-root, no-network
container is permitted preparation; it starts no backend and executes no test.
Full original e2e execution still requires the provisioned disposable dependency
and Haskell-peer environment. Do not run against shared/production host services.

The user's request to complete that Docker setup and run automated original tests
authorizes full original proxy/cargohold e2e execution only in the task-owned
[private environment](docs/go-migration/plans/e2e-environment.md): internal Docker
networks, no host ports, synthetic fixtures, owned dependency volumes and pinned
Haskell peers. This narrowly scoped exception overrides the general integration
execution prohibition for this environment; shared/production targets remain
prohibited. Missing baseline reports cannot qualify the setup or a Go candidate.
The same user's explicit self-review and autonomous-development delegation applies
to this bounded environment task; label same-author review honestly and retain
all substantive test/evidence requirements. This does not approve service
contract exceptions or production switches.
