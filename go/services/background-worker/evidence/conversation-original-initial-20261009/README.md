Original MLS migration initial execution: fail

Exact clean static Go1.26.9 executable/source identity is in build.json; original
unchanged Migration.Conversation.testMigrationToPostgresMLS ran with only Go
background-worker and all other Haskell peers. It reached phase3 with conversation
migration enabled, then timed out at Test/Migration/Util.hs:39 / Conversation.hs:90.
Metrics reported local32/remote2 migrated, finished1/failed0, but the original
wait/assert helpers require Haskell counter text1.0/0.0, not Go integer1/0.
This is a candidate output contract mismatch, not a baseline waiver. No phase4/5
or final shared-history proof. Preserve failed evidence and fix candidate metrics
before rerunning the same original case. Controller2/runner1/cleanup0; owned
container/network/volume labels empty after cleanup. No live stack remains.
