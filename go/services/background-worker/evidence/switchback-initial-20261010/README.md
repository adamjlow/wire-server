Initial actual durable rehearsal, sourcea505d4c1c: reference Haskell claims the
meeting job (attempt1/owner/lease), Rabbit notification1unacked/1ready and queued
conversation/partial migration state are captured. With the synthetic meeting
row lock held indefinitely, native SIGTERM fails to exit within35s. No Go owner
started. Cause is not independently isolated; source cleanup cancels/drains
workers, and this observation occurs with a blocked PostgreSQL handler.
Controller1, owned forced cleanup0, exact-project resources empty. This is a
retained reference/fixture interruption, not candidate success or a waived Go
shutdown criterion. Subsequent rehearsal explicitly bounds the initial native
stop like container-orchestrator termination, recording any SIGKILL137 and exact
retained lease before starting Go. Go's required clean loaded SIGTERM already
passes in the separate declared operations smoke and is unchanged.
