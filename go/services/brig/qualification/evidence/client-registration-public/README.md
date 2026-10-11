# Public client registration composition — scoped proof

Production source `a3980be43743ab031e9f2f62a44902b845d7d654`, executable
`fd9052f985ceec69d3260bceb94130eb75a84509e77ee13f303a1dc205ac9eac`,
passes full pinned Go1.26.9 SDK checks and static normal units in the
unchanged original runner image. Tagged stores compile only. Both managed
containers exit0/network removal0/independent exact-name audits empty.
Existing static glibc/OpenSSL linker warnings retained; native Haskell builds
were not run. Fresh external outputs are in the identified owned runtime
temporary directory; SDK/cache/compiler/runner identities unchanged.

Native sources: Wire.ClientSubsystem.Interpreter.addClient/upsertClient;
AuthenticationSubsystem.Interpreter.enforceVerificationCodeImpl;
UserSubsystem.getAccountNoFilter/getByNoFilters/getAccountsByImpl;
Wire.API.User.Client.newClientSchema and public Brig client routes;
Brig.Options.defUserMaxPermClients; EmailSubsystem.Interpreter NewClient
renderer; Wire.API.Locale calendars and Data.Id.defRequestId.

POST/clients is now bound to actual account/code/CQL/auth/client store,
configured consumable callback, old-client deletion execute, actual Galley
and synchronous V6 client-added event and real template/SES/SMTP mail sender.
It rejects manual legalhold before account lookup. Account projection has
no identity/status/expired-invitation filter, preserving native getNoFilter.
Separate initial/email-team/SAML lookup points preserve native ordering even
when the verification feature is disabled. Team feature or disabled non-team
default, SAML bypass, Login scope, required/no-email/invalid-code precedence
use existing real verification stores. No new authentication policy added.

Native optional setUserMaxPermClients/default7 binds the existing registration
core. Storage writes precede consumable setup, old-client deletion, Galley,
synchronous event and mail. Mail uses the original initial account/email and
pre-deletion total>1, including temporary replacement; missing identity does
not acquire an invented rejection. Subsequent failures leave prior effects
committed, as native does. New response201 includes ID-only Location, versioned
capabilities, millisecond creation time and no last_active.

New-client mail uses the original repository user/new-client templates,
branding/name/label/model/date parameters and N/A defaults for absent label/
model. DE/FR calendars and English fallback match the source-specific date
format; receiver name and X-Zeta-Purpose NewDevice retained. Actual sender
is the existing awaited SES/SMTP transport; this slice's mail evidence is
rendering/binding, not new-client native delivery comparison.

Finite tests cover successful and setup/deletion/Galley/event/mail failure
order, account lookup counts, missing/code/feature/SAML/public-legalhold
admission, versioned public response, native configured-limit YAML, actual
repository template/defaults/envelope and calendar formatting. Prior actual
HTTP peer/SQS/Dynamo controls remain passing in full normal/race/static units.

No native backend registration/deletion state switchback, new-client mail
delivery or original Client test execution is claimed. Next use original
compiled Client.testCreateClientWithCapabilities,
Client.testUpdateClientWithConsumableNotificationsCapability and
Client.testGetClientCapabilitiesV7 in the owned harness, plus actual native
registration/deletion/error/partial-state witness. Last-active callback is
needed for Client.testClientLastActive; cross-backend offline case still needs
connection/federation. Internal legalhold add/disable/capability augmentation/
events and Go internal-deletion consumer/STOMP remain whole-service work.
Whole535/eight-Go/all-entity3mode/ops/image/selector/legal/security/review
qualification remains incomplete, Brig unavailable and Galley not started.

Same-author slice review under explicit delegated self-approval accepts this
implementation within the stated finite scope; not independent/whole-service
approval. Preserve inherited identityless/status admission, reauth exemptions,
nontransactional storage/peer/mail effects and retained MLS rows. No Haskell,
oracle/fixture/deadline/disk-floor/default/schema/security-policy change.
