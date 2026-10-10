// SPDX-License-Identifier: AGPL-3.0-only
// Private switchback fixture: original schemas, bound synthetic values only.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
)

const user = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
const returning = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2"
const conv = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa3"
const meeting = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa4"

func run() error {
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" || len(os.Args) != 2 {
		return fmt.Errorf("owned fixture required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	pg, err := pgx.Connect(ctx, "postgres://wire-server:posty-the-gres@127.0.0.1:5432/backendA?sslmode=prefer")
	if err != nil {
		return fmt.Errorf("fixture PostgreSQL unavailable")
	}
	defer pg.Close(context.Background())
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.Keyspace = "brig_test"
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 5 * time.Second
	cass, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("fixture Cassandra unavailable")
	}
	defer cass.Close()
	exec := func(sql string, args ...any) error {
		_, err := pg.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("fixture statement failed: %s", sql)
		}
		return nil
	}
	seedUser := func(id, name string) error {
		uid, err := gocql.ParseUUID(id)
		if err != nil {
			return err
		}
		return cass.Query(`INSERT INTO user (id,name,activated,accent_id,user_type) VALUES (?,?,?,?,?)`, uid, name, true, 0, 0).WithContext(ctx).Exec()
	}
	seedMeeting := func() error {
		if err := exec(`INSERT INTO conversation(id,type,protocol,group_conv_type) VALUES($1::uuid,0,0,0) ON CONFLICT DO NOTHING`, conv); err != nil {
			return err
		}
		if err := exec(`INSERT INTO meetings(id,title,creator,start_time,end_time,tzid,conversation_id) VALUES($1::uuid,'synthetic switchback',$2::uuid,NOW()-INTERVAL '3 hours',NOW()-INTERVAL '2 hours','UTC',$3::uuid)`, meeting, user, conv); err != nil {
			return err
		}
		hash := sha256.Sum256([]byte("meeting:" + meeting))
		key := base64.RawURLEncoding.EncodeToString(hash[:15])
		if err := exec(`INSERT INTO conversation_codes(key,conversation,value,expires_at,target) VALUES($1,$2::uuid,'synthetic',NOW()+INTERVAL '1 hour','meeting')`, key, meeting); err != nil {
			return err
		}
		return exec(`INSERT INTO arbiter.meetings(payload,archive_for,dedup_key,not_visible_until) VALUES($1::jsonb,3600,'synthetic-switchback-meeting',NOW()+INTERVAL '1 hour')`, `{"type":"meetings_cleanup","data":{}}`)
	}
	switch os.Args[1] {
	case "seed":
		if err := seedUser(user, "synthetic Cassandra source"); err != nil {
			return err
		}
		// Explicit previous-partial-copy fixture, not claimed to be native-generated.
		if err := exec(`INSERT INTO wire_user(id,name,activated,accent_id,user_type,created_at) VALUES($1::uuid,'synthetic preserved partial copy',true,0,0,'2026-10-01T00:00:00Z')`, user); err != nil {
			return err
		}
		if err := exec(`INSERT INTO user_migration_pending_deletes(id) VALUES($1::uuid)`, user); err != nil {
			return err
		}
		if err := seedMeeting(); err != nil {
			return err
		}
		if err := exec(`INSERT INTO arbiter.conversations(payload,archive_for,dedup_key,not_visible_until) VALUES($1::jsonb,3600,'synthetic-switchback-conversation',NOW()+INTERVAL '1 hour')`, `{"type":"adminless_deletion","data":{"team_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1","conversation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa3","request_id":"synthetic-switchback"}}`); err != nil {
			return err
		}
	case "activate-meeting":
		if err := exec(`UPDATE arbiter.meetings SET not_visible_until=NOW() WHERE dedup_key='synthetic-switchback-meeting'`); err != nil {
			return err
		}
	case "release-scheduled":
		if err := exec(`UPDATE arbiter.conversations SET not_visible_until=NOW() WHERE dedup_key='synthetic-switchback-conversation'`); err != nil {
			return err
		}
	case "seed-return":
		if err := seedUser(returning, "synthetic Haskell continuation"); err != nil {
			return err
		}
		if err := seedMeeting(); err != nil {
			return err
		}
	case "lock-meeting":
		tx, err := pg.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.Exec(ctx, `SELECT id FROM meetings WHERE id=$1::uuid FOR UPDATE`, meeting); err != nil {
			return err
		}
		fmt.Println(`{"lock":"held"}`)
		<-ctx.Done()
		return nil
	case "snapshot":
		result := map[string]any{}
		for name, sql := range map[string]string{
			"meeting_jobs":         `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM arbiter.meetings t`,
			"conversation_jobs":    `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM arbiter.conversations t`,
			"meeting_archive":      `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM arbiter.meetings_archive t`,
			"conversation_archive": `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM arbiter.conversations_archive t`,
			"meetings":             `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM meetings t`,
			"codes":                `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM conversation_codes t`,
			"users":                `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM wire_user t`,
			"pending":              `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]') FROM user_migration_pending_deletes t`,
		} {
			var raw json.RawMessage
			if err := pg.QueryRow(ctx, sql).Scan(&raw); err != nil {
				return fmt.Errorf("fixture snapshot %s failed", name)
			}
			result[name] = raw
		}
		var ids []string
		iter := cass.Query(`SELECT id FROM user`).WithContext(ctx).Iter()
		var id gocql.UUID
		for iter.Scan(&id) {
			ids = append(ids, id.String())
		}
		if err := iter.Close(); err != nil {
			return err
		}
		if ids == nil {
			ids = []string{}
		}
		result["cassandra_users"] = ids
		return json.NewEncoder(os.Stdout).Encode(result)
	default:
		return fmt.Errorf("unknown fixture action")
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
