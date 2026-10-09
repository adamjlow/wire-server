// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"encoding/binary"
	"errors"
	"time"
)

func uuidMigrationLockKey(id string) (int64, error) {
	u, err := postgresUUID(id)
	if err != nil {
		return 0, err
	}
	w1, w2 := binary.BigEndian.Uint64(u.Bytes[:8]), binary.BigEndian.Uint64(u.Bytes[8:])
	return int64(w1 ^ (w2 >> 32) ^ (w2 << 32)), nil
}

func (s *postgresStore) WithConversationLock(ctx context.Context, id string, exclusive bool, action func() error) error {
	return s.withMigrationLock(ctx, id, exclusive, 500*time.Millisecond, action)
}

func (s *postgresStore) withMigrationLock(ctx context.Context, id string, exclusive bool, timeout time.Duration, action func() error) error {
	key, err := uuidMigrationLockKey(id)
	if err != nil {
		return err
	}
	// The source's timeout includes pool acquisition. A separate session holds
	// the advisory lock throughout cleanup and operations on the normal pool.
	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := s.Pool.Acquire(acquireCtx)
	if err != nil {
		return errors.New("migration lock acquisition failed")
	}
	lockSQL, unlockSQL := `SELECT pg_advisory_lock_shared($1::bigint)`, `SELECT pg_advisory_unlock_shared($1::bigint)`
	if exclusive {
		lockSQL, unlockSQL = `SELECT pg_advisory_lock($1::bigint)`, `SELECT pg_advisory_unlock($1::bigint)`
	}
	if _, err = conn.Exec(acquireCtx, lockSQL, key); err != nil {
		// Cancellation may have won just after the server acquired the lock.
		// Close the session rather than returning possibly-held locks to pool.
		closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
		_ = conn.Conn().Close(closeCtx)
		stop()
		conn.Release()
		return errors.New("migration lock acquisition failed")
	}
	defer func() {
		unlockCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		var unlocked bool
		if err := conn.QueryRow(unlockCtx, unlockSQL, key).Scan(&unlocked); err != nil || !unlocked {
			_ = conn.Conn().Close(unlockCtx)
		}
		conn.Release()
	}()
	return action()
}

func (s *postgresStore) ConversationExists(ctx context.Context, id string) (exists bool, err error) {
	cid, err := postgresUUID(id)
	if err != nil {
		return false, err
	}
	err = retryPostgres(ctx, s.Pool.Reset, func() error {
		conn, err := s.acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation WHERE id=$1::uuid)`, cid).Scan(&exists); err != nil {
			return postgresFailure(err, "conversation existence query failed")
		}
		return nil
	})
	return
}
