# Activation API orchestration checkpoint

Source `ddfa283d51f50f281689b57b346582a0024dd78d`. Pinned Go1.26.9 formatting/module/vet/unit/race/build/vulnerability/license checks and static units in the unchanged runner pass. Candidate `1b578b76c1afa64200168f7f1ab560ade5ba6cb50aa6a90083ca0ba7adb1d74d`; unit executable `1b254da8697c109405785578a4a923e37fa38593a91683128ee81b0c46a42299`. Owned controller and independent resources are empty after cleanup.

Focused tests cover first activation, email update, repeated-link no-op, dry-run, internal domain bypass, failures at verification/domain/mutation/index/event and missing downstream dependencies. Mutation errors cannot expose a successful result. Ordering follows native preverification/domain before mutation, search before event, then team activation or additional unvalidated-email clearing.

No database/query change occurred in this increment; native-schema results remain bound to the previous exact source/artifacts in ../domain-registration. The tagged store binary was compiled but not executed for this increment. This is orchestration/unit proof, not public activation HTTP, notification/journal/team integration, original e2e or whole-service parity. Complete actual downstream implementations before HTTP exposure, and retain full Brig gates. Galley remains authorized only after whole Brig completion.
