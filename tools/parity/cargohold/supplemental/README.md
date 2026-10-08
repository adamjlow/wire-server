# Finite cargohold supplement

Reference-owned assertions implementing the completed service plan's finite
matrix. Original compiled tests remain untouched. Current runner executes 25
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
