# Principles

1. Keep upstream-compatible Haskell supported and default. Add Go alternatives
   without changing existing users' build, installation or upgrade behaviour.
   Reproduce observable behaviour using idiomatic Go, not Haskell source structure.
2. Let operators opt in at independently deployable service boundaries. Mixed
   Haskell/Go deployments are normal; switching one deployment never retires a
   Haskell service from the repository. Preserve a tested switchback path.
3. Treat API responses, queues, storage effects, config and operations as one
   contract. A passing HTTP happy path is insufficient.
4. Keep the oracle independent. Bugs or unsafe behaviour discovered in the
   reference become explicit decisions, never silent compatibility exceptions.
5. Prefer small tasks with narrow write ownership, measurable gates and durable
   evidence. Automate mechanical checks; retain human decisions at high-impact
   contract and deployment boundaries.
6. Learn once. Record evidence and uncertainty near the service. An unchecked
   template or task status is not evidence, and passing a validator is not parity.
7. Keep the fork easy to update: isolate additions, minimize upstream changes and
   revalidate Go compatibility as upstream evolves. Record exact tested revisions;
   availability is not a promise of compatibility with every future upstream release.
