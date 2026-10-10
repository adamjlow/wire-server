# Owned Docker orphan Nginx supervision

The unchanged published original test runner sends SIGINT to native service
masters, waits50ms, then SIGKILLs the master if it has not exited. Actual
original UserMigration and DomainRegistration failures show orphan native Nginx
workers still listening on10099 after that cleanup. Haskell production and
original assertion fixes are outside this task.

Add one explicit optional environment-run flag. Run a small nonroot PID1
supervisor inside the existing disposable container. It may signal/reap only
adopted native Nginx worker processes with matching UID, executable and pidfd
identity; healthy workers still parented to a live master remain untouched.
Bound cleanup, retain signal/process identities and fail unsafe contexts. No
new host permissions/network/mounts, defaults, original assertions/catalogues,
service deadlines, reference bytes, service code or production switch.

Finite proof uses two actual pinned native Nginx masters in an owned Docker
container. Kill one master exactly as the existing native test runner can;
prove its listener closes after orphan cleanup and the other healthy worker
continues HTTP200. Retain actual PIDs/executables/signals/hashes/stops/cleanup.
Check host guard rejects and existing harness tests pass. Delegated same-author
review must describe the cleanup scope honestly, without independent-review or
native-build claims. Then Spar may execute only its remaining complete47-case
worker suite with unchanged seven artifacts and original assertions.

Initial queue/claim validation rejected because this required plan file had not
yet been created; the shell continued committing both metadata steps. Retain
that failed validation openly. This follow-up supplies the immutable declared
plan path and revalidates the active claim before any implementation/probe.

Initial owned native probe at8c6bb8d51 stopped before proving orphan behavior: Python helper function http shadowed imported http.client. Runner1/cleanup0/empty and exact source receipts retained in qualifications/initial. Alias the imported client explicitly; no reference/assertion change, corrected actual probe required.

Second actualprobe/f34173ed3 fails nativeNginx startup: Wire custom module requires zauth_keystore, so bare generic Nginx fixture is invalid. Retain actualmissing-keystore error/runner1/cleanup0/empty in native-config-initial. Configure repository synthetic public auth keystore/ACL/OAuth fixture and native health-location zauth-off, as originalfixture; no original/protected serviceconfig edits or productionauth change. Corrected nativeprobe stillrequired.

Qualified54ebf63fa actualnativeNginx55fff259 masterSIGKILL leavesorphanworker9/parent1/listener18090; optionalwrapperSIGTERMworkerexits0/reaps/releaseslistener. Healthymaster10/worker remainsHTTP200 andstops0. Probe/wrapper/cleanup0/independentlyemptyproject3eb3034e797d4b638a6ed4cb5520565c. Hostguardreject2/all27harnesstests pass. Exactcurrentreceipts retained qualified/; failedfixtures retained. Same-author user-delegated review approves opt-in harness-only cleanup. No Haskell/default/assertion/timeout change or Sparavailability claim.
