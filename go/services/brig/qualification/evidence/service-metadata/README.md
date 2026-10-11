# Native service metadata and final storage cleanup

Source `f42118429705a8898b855a75d0daa6a4743289be`, service
`6b357d1abf6acc40c86b291a1a4ee8a51b391cd0bb9afa1153ebd562d5107f3b`:
[pinned Go checks](sdk/go-check.log), [static original-runner units](sdk/runtime-units.log)
and [build receipt](sdk/build-receipt.json) pass. The exact tagged stores
`d0aa8113a54ce34e23051312576b7e3787447da7376606c211c0661c62aadb93`
execute against the native-created schema: [receipt](stores/result.json),
[assertions](stores/test.stdout), [schema output](stores/native-schema.stdout).
Prior account/group/code/connection/bot controls also pass.

Metadata lookup decodes the same nine columns as native lookupService, rather
than hiding malformed fields behind a name/tags-only projection. Required
name/description/enabled, HTTPS URI, nonempty tokens/keys, pubkey type/size/single
PEM envelope, assets and tag enum bits are validated. Optional summary and empty
asset/tag collections are retained. Storage errors never expose tokens/PEM/URLs.
PEM envelope admission does not add RSA cryptographic validation absent from
this native read path. Native stored tokens retain their permissive ASCII wrapper;
no new token security policy is imposed.

Actual schema controls verify ordinary metadata, missing service, null name and
enabled, non-HTTPS URL, missing tokens/keys and invalid enum rejection. Logged
reverse/forward whitelist deletion precedes an unlogged service/prefix/tag batch.
Both whitelist directions and all seven subsets of three tags disappear;
unrelated service index/whitelist rows remain. Metadata cleanup replay succeeds.
Literal dotted-I/sigma index keys independently exercise native lowercase
expansion; unit tests also preserve non-contextual Greek sigma. Source:
[Data.Text case conversion](https://raw.githubusercontent.com/haskell/text/master/src/Data/Text.hs).
Existing pinned golang.org/x/text0.42.0 implements that mapping with final-sigma
handling disabled. Each operation creates a private caser for concurrency safety.
No dependency version changed; text is now explicitly a direct dependency.

The [initial SDK check](initial-sdk/build-receipt.json) failed only dependency
alignment after using text directly; [diagnostic](initial-sdk/go-check.log) is
retained. Corrected SDK/static/schema/test exits and cleanup are0; an
[independent audit](cleanup.json) finds no owned resources. Existing static linker
warnings remain in [controller output](sdk/controller.log). No Haskell code,
protected oracle, default selection or host dependency changed.

Scope limits: this is candidate real-store/source-derived contract qualification,
not compiled-native metadata API comparison or new backend e2e. URL/PEM admission
and Unicode-version differences still need consideration in actual provider
comparisons. More than three valid native tags is corrupt/outside the documented
service bound: candidate reports a generic error before mutations, where native
unsafeRange/unfoldTags can fail after whitelist cleanup. Native whitelist cleanup
builds one batch covering every listed team; very large associations retain a
potential availability/resource concern. These differences/risks are recorded,
not approved contract exceptions. No whole-service parity is claimed here.

The typed Galley bot/service adapters and queue service.delete binding remain
pending, followed by actual native-Go-native replay. Prior original22/ActiveMQ
qualification retains its older exact tuple. Same-author review under the user's
existing delegation accepts this bounded increment only. Whole535/eight-Go/
all-entity/operations/image/selection gates remain incomplete; no push occurred.
