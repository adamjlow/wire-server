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
