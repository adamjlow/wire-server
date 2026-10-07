# <service> compatibility contract

Contract task/review: <id/link>; reference SHA/image: <immutable identifiers>.
Status: draft until separately approved by a human.
Imported upstream SHA: <full SHA>; candidate SHA/image: <when available>.
Haskell remains the supported default and reference throughout.

## Observable matrix

| Scenario / API version | Inputs / preconditions | Response and effects | Evidence / fixture |
| --- | --- | --- | --- |
| <scenario> | <synthetic values> | <precise semantics> | <source/test reference> |

Cover successful and malformed/unauthorized requests, error codes and bodies,
serialization, headers, auth, federation, WebSockets, queues, storage, ordering,
retries, timeouts and partial failure. Explain applicability per interface.

## Operational contract

Record config and defaults, routing/discovery, startup/health/shutdown,
observability, supported peer versions, resource and performance thresholds,
load model, rollback constraints and observation windows.
Define omitted versus explicit implementation selection, invalid-choice handling,
existing values/image overrides, supported mixed deployments and upgrade versions.
Require data-compatible Haskell-to-Go-to-Haskell switching with upstream schemas;
Go availability must not change Haskell build or installation requirements.

## Oracle and exceptions

List protected paths in the service record before implementation claims them.
Pin fixture/normalizer revisions. For each allowed nondeterminism, cite a reviewed
decision, comparator rule and negative regression test. Document reference bugs
and explicit contract changes separately; no silent exclusions.

## Required evidence

Map each behaviour to scenarios and required checks, exact commands, environment,
coverage requirements and artifact format. List unknowns/blockers and security
requirements. Approval does not imply tests have been executed.
