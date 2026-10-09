Scheduled queue storage construction (not yet connected)

Full Go1.26.9 format/tidy/verify/vet/race/unit/build/vulnerability/license checks
passed for source-files.json inputs. No new Go dependency. SQL adaptation is from
pinned velveteer/arbiter b9c57eb1f8277d97616aa449bea471fe9ce14eda,
Arbiter.Core.Sql.Claim and .Lifecycle. Its original MIT LICENSE and copyright
notices are retained in arbiter_sql; include that notice in the eventual image.
Primary license: https://raw.githubusercontent.com/velveteer/arbiter/b9c57eb1f8277d97616aa449bea471fe9ce14eda/LICENSE

Wire.API.Jobs scheduled {type,data} parsing is separate from Rabbit envelopes.
Supported queue/tag matching, required identifiers/request/time fields, optional
actor and source extra-field tolerance are covered by focused tests. Fields from
another tag are ignored instead of accidentally validating unknown extra input.
Meetings cleanup accepts the native empty object with extra fields.

Claim SQL adapts native no-admission-policy/single-job batching: group candidate
locking, priority/FIFO slot allocation, SKIP LOCKED jobs and atomic attempt/lease/
worker updates. Native heartbeat and retry SQL retain optimistic attempt guards
and return heartbeat/stolen/cancellation state. Queue names are restricted to the
two static registered queues; every value is bound. Security tests reject foreign
or injected queues before any PG access and invalid UUID/unbounded claim budgets.

These methods compile and pass unit/race checks; they have NOT been exercised
against actual PostgreSQL, and unit checks do not prove DB locks or concurrency.
Scheduled runtime remains disconnected until schema migrations, worker registration,
ACK/archive/retry/reaper/cron and actual adminless/meetings handlers are complete.
Protected schema/reference assertion material is unchanged. No final artifact or
scheduled/full-service qualification claim.

An independent original gundeck40 run is live on earlier exact clean source
00557161c5218fba7ce290ba57536561298a35e3 and SHA256
80b1f77974affea10b17d55a795c95126f9e2992d9848280c6af6569c01b7f92.
These new scheduled construction sources are not in that executable. Preserve
that distinction when recording its terminal receipt and cleanup.
