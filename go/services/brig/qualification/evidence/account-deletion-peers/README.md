# Account deletion peer adapters

Tested source `a408e14a4b611f8c486a4625e0734731554fe2b3`; static service
`76ee2284b3d4975487d4ced887f4a9cf6ee22a2eced48784908a200a4bafac73`.
[SDK receipt](build-receipt.json) identifies the pinned compiler, SDK, original
runner, static unit executable and compiled tagged-store executable.
[Go checks](go-check.log) pass formatting, dependency alignment/verification,
vet, unit/race, build, vulnerability and license scans. No new dependency.
[Static runner tests](runtime-units.log) pass in the unchanged original runner.
Tagged stores compile only; no new durable-store or backend integration claim.
[Cleanup](cleanup.json) records controller completion and independent empty
owned containers/networks/volumes; scoped old compiler-cache maintenance removed
no retained artifacts.

Native sources: `Brig.IO.Intra.rmUser`, `Wire.GundeckAPIAccess.UserDeleted`,
`Wire.GalleyAPIAccess.Rpc.memberIsTeamOwner`, `Wire.Rpc.x3` and
`Brig.RPC.serviceRequestImpl`. Owner lookup permits deletion only on403;
Gundeck/Galley cleanup requires2xx before asset removal. Native asset cleanup
ignores HTTP status, including5xx, and retries transport errors only. Keep
100/200/400ms retries, request/user headers and original pre-tombstone assets.

HTTP tests demonstrate owner200/204/403/404/500, cleanup order, exhausted Gundeck
retries, Galley403 short circuit, asset404/503 without retry, and wire headers.
Malformed asset keys are rejected before peer side effects; response draining
is bounded and canceled transport requests do not continue retries. Error text
does not expose peer response bodies or transport details.

Same-author review under the user's existing delegated self-review accepts this
bounded adapter increment; no independent review is claimed. The ordinary
license scanner local-module URL warning is retained; repository LICENSE is
unchanged and this increment adds no license/dependency surface. No Haskell,
protected assertion, fixture, selector or service gate changed.

Adapters are not yet attached to public deletion. Group cleanup, event fanout,
persistent Rabbit messages, verification/mail and internal replay remain before
DELETE/self can qualify. The prior original22 failure remains unresolved and
whole535/eight-Go/service operations/switchback acceptance remains mandatory.
