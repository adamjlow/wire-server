# Deletion verification completion and native-schema stores

Source `0dbc1e25879b668fe210b1f1b7f016c18047aa6e`, service
`993a433917d68dded7c52bfbb416117d8ce278fa457cc77a2af7d2c9bd21b2f1`:
[full pinned Go checks](sdk/go-check.log) and
[static original-runner units](sdk/runtime-units.log) pass.
[Build receipt](sdk/build-receipt.json) records exact SDK/archive/toolchain/artifacts.

Verification completion preserves native getAccountNoFilter, tombstone cleanup
replay, missing-account code consumption and invalid unbound-code rejection.
Unit tests prove request identity and cleanup-before-code-removal ordering;
failed cleanup leaves the code for replay. This core is not public HTTP-bound.

Actual native-schema [stores](stores/result.json) pass in2.06 seconds using tagged
binary `6b39907dce88002221fa169c306d6e1c0ca586af40ff335f6a732eef60f120e6`.
New controls verify real scope1 value/email/account/TTL fields, wrong-attempt
retry reduction retaining remaining TTL, successful verification retaining the
row, throttle TTL and its retention after code removal, isolation from scope6,
native Int32→Word8 exhausted-retry wrapping and required stored-TTL rejection.
Existing three-selector user/account/group/client and remote cleanup controls
remain green. Native schema utility, candidate and cleanup exit0. This does not
prove native HTTP responses, actual SMTP/SES delivery or original e2e assertions.

[Cleanup](cleanup.json) records terminal handles and independent empty SDK/store
resource audits. [Lossless maintenance](lossless-maintenance.json) records nine
owned superseded executable archives, original/compressed hashes, ownership/mode
and restoration instructions; every decompressed hash was checked before removing
its uncompressed duplicate. No historical artifact was discarded.

Same-author review under the user's existing delegation accepts this increment;
no independent or whole-service approval is claimed. Public deletion HTTP,
complete engine startup binding and actual internal queue replay remain. Final
original535/eight-Go, operations, all-entity switchback and image/selector gates
remain outstanding. Haskell sources, protected assertions and defaults unchanged.
