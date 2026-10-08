# Finite cargohold supplement

Reference-owned assertions implementing the completed service plan's finite
matrix. Original compiled tests remain untouched. Current runner executes 38
cases against actual pinned Haskell and the identified candidate in a private
Docker/Garage environment with a synthetic Brig response and shared test bucket.
It uses no host backend dependencies, production targets or customer fixtures.

The initial observed run (source 8eded0536bbe5a3c3853ea45c12d69aa496089df)
proves six bidirectional user/bot/provider byte/read/delete scenarios; user token
renewal invalidates the old token across runtimes. Parser error messages,
malformed UUID/token handling and application JSON content types expose genuine
candidate differences. Raw JSON object ordering/newlines in that initial report
are incidental; the controller now compares decoded JSON while preserving exact
error strings, content type, status and effects. Signed URLs are verified against
the owned store before specific date/signature normalization. Fixed query fields
are not stripped. The initial internal URL observations were not fetched and are
explicitly unverified; raw signatures were removed before committing captures.

This is observation/characterization, not passing full-service parity. Remaining
matrix cases and focused runner unit checks are still work for this active task.
Stop at the already declared finite matrix. Retain reports for failed attempts;
do not relabel them as passes or widen normalization to fit the candidate.

```sh
python3 tools/parity/cargohold/supplemental/runner.py --binary /absolute/cargohold --output /absolute/new-directory
```

A process exit zero means observations were captured and switchback hard checks
passed. Inspect every `equal`/mismatch value before a compatibility claim; no
successful process exit converts mismatches to passing evidence. All owned
resources are removed in finally; nonzero cleanup propagates failure.

The next retained [25-case observations](core-observations.json) and
[run identity](core-run.json) have no mismatches at candidate source
bcbcd03b01f22a9f2e751b4c7385467a5912d1e3. Parser error strings and malformed
token handling now match; JSON content types and presigned fixed query parameters
were corrected in candidate code. Signed internal URLs were actually fetched
before narrow clock/signature normalization. This remains a partial matrix,
explicitly whole_service_parity false. Owned controller cleanup succeeded.

[Strict signed-field rerun](core-strict-observations.json), with
[identity](core-strict-run.json), again has all 25 cases matching. Both redirect
and internal URL comparisons now retain every fixed query field and validate
signing algorithm, date/credential agreement, scope, headers and expiry before
normalizing the verified clock/signature. Two focused unit checks ensure invalid
reads/dates/scopes/algorithms fail and fixed fields/verified payloads survive.
No native runtime counters, broad JSON bodies or authorization fields are stripped.

The first 38-case run adds retention variants, principal precedence and audit
metadata/log effects. [Observations](audit-initial-observations.json) expose three
candidate differences: audit metadata encoding, null conversation handling and
audit event fields. These failures are preserved; 35 matches do not establish
whole-service parity.

The repaired [38-case run](audit-observations.json) now matches all cases at
candidate source 803ad4e59de9d48348af1902f1f0c41458937f81; [runtime identity](audit-run.json)
records the binary hash and controller checkout. Changes made during that run
added optional packaging only; the mounted candidate was unchanged. Audit S3
metadata bytes, null rejection and request/event log fields now match without
normalizing away those differences. Retention key/expiry/token relationships and
principal precedence also match. The finite whole-service matrix remains incomplete.

`--group extended` executes the planned 20 replay cases plus streaming, token,
disabled-version and five S3 fault cases. [First 35-case capture](extended-initial-observations.json)
and [identity](extended-initial-run.json) retain four failing observations: gzip
parser diagnostics, DELETE token handling and two generic 500 messages. Real S3
abort effects already match. The 24 MiB upload persists identical bytes and metadata.

The repaired [35-case extended run](extended-observations.json) and [identity](extended-run.json)
match all cases at candidate source 3c9e31c160fa86df86bf85c3f9b7c4351153c063.
Gzip diagnostics, DELETE header parsing and failure messages were corrected in
candidate code; assertions and failure effects were preserved. Core 38 evidence
applies to its earlier binary and will be rerun after the remaining relevant work.
