//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Supplemental queue fixture: native HTTP creates the user, clients, prekeys,
// MLS keys and properties. This helper only attaches native service rows and
// independently reads raw durable effects. It never executes candidate cleanup.
func TestOwnedServiceDeletionFixture(t *testing.T) {
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("owned nonroot runner required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := LoadConfig(os.Getenv("BRIG_STORE_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := openCassandra(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var input struct{ Action, Provider, Service, User, Conversation, Team, Output string }
	raw, err := os.ReadFile(os.Getenv("BRIG_SERVICE_ROWS_INPUT"))
	if err != nil || len(raw) > 4096 {
		t.Fatal("invalid service fixture manifest", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		t.Fatal(err)
	}
	ids := make([]gocql.UUID, 5)
	for i, value := range []string{input.Provider, input.Service, input.User, input.Conversation, input.Team} {
		ids[i], err = gocql.ParseUUID(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	provider, service, user, conversation, team := ids[0], ids[1], ids[2], ids[3], ids[4]
	exec := func(statement string, args ...any) {
		t.Helper()
		if err := s.Query(statement, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec(); err != nil {
			t.Fatal(err)
		}
	}
	mode := c.Migration["user"]
	if mode != "cassandra" && mode != "postgresql" && mode != "migration-to-postgresql" {
		t.Fatal("invalid user backend")
	}
	// Native HTTP writes PostgreSQL in both PostgreSQL and migration modes.
	// Attach bot fields from Provider.API NewStoredUser: active0, bot1,
	// activated, no expiry. The unassociated /i/users actor is ephemeral3.
	// Never synthesize a Cassandra account when the native actor lives in PG.
	var accountProvider, accountService string
	var status, userType int32
	var name string
	counts := map[string]int{}
	postgres, err := openPostgres(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer postgres.Close()
	if input.Action == "stage" {
		if mode == "cassandra" {
			if err := s.Query(`SELECT name FROM user WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&name); err != nil || name != "Synthetic service queue bot" {
				t.Fatal("native Cassandra actor missing", err)
			}
			exec(`UPDATE user SET provider=?,service=?,status=?,user_type=?,activated=?,expires=? WHERE id=?`, provider, service, int32(0), int32(1), true, nil, user)
			exec(`INSERT INTO service_user (provider,service,user,conv,team) VALUES (?,?,?,?,?)`, provider, service, user, conversation, team)
			exec(`INSERT INTO service_team (provider,service,team,user,conv) VALUES (?,?,?,?,?)`, provider, service, team, user, conversation)
		} else {
			if err := postgres.QueryRow(ctx, `SELECT name FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&name); err != nil || name != "Synthetic service queue bot" {
				t.Fatal("native PostgreSQL actor missing", err)
			}
			result, err := postgres.Exec(ctx, `UPDATE wire_user SET provider=$1::uuid,service=$2::uuid,account_status=$4,user_type=$5,activated=$6,expires=NULL WHERE id=$3::uuid`, provider.String(), service.String(), user.String(), int32(0), int32(1), true)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatal("native PostgreSQL actor association", err)
			}
			if _, err := postgres.Exec(ctx, `INSERT INTO bot_conv (id,conv,conv_team) VALUES ($1::uuid,$2::uuid,$3::uuid)`, user.String(), conversation.String(), team.String()); err != nil {
				t.Fatal(err)
			}
		}
		// Native public test key from Wire.API.Provider.Service's documented
		// example. Public material only; no private key or generated secret.
		pem := []byte("-----BEGIN PUBLIC KEY-----\n" +
			"MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAu+Kg/PHHU3atXrUbKnw0\n" +
			"G06FliXcNt3lMwl2os5twEDcPPFw/feGiAKymxp+7JqZDrseS5D9THGrW+OQRIPH\n" +
			"WvUBdiLfGrZqJO223DB6D8K2Su/odmnjZJ2z23rhXoEArTplu+Dg9K+c2LVeXTKV\n" +
			"VPOaOzgtAB21XKRiQ4ermqgi3/njr03rXyq/qNkuNd6tNcg+HAfGxfGvvCSYBfiS\n" +
			"bUKr/BeArYRcjzr/h5m1In6fG/if9GEI6m8dxHT9JbY53wiksowy6ajCuqskIFg8\n" +
			"7X883H+LA/d6X5CTiPv1VMxXdBUiGPuC9IT/6CNQ1/LFt0P37ax58+LGYlaFo7la\n" +
			"nQIDAQAB\n-----END PUBLIC KEY-----\n")
		keys := []map[string]any{{"typ": int32(0), "size": int32(2048), "pem": pem}}
		exec(`INSERT INTO service (provider,id,name,summary,descr,base_url,auth_tokens,pubkeys,assets,tags,enabled) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, provider, service, "Synthetic queue service", "Synthetic summary", "Synthetic description", []byte("https://example.com/synthetic-service"), []string{"c3ludGhldGlj"}, keys, []map[string]any{}, []int64{1, 2, 4}, false)
		exec(`INSERT INTO service_whitelist (team,provider,service) VALUES (?,?,?)`, team, provider, service)
		exec(`INSERT INTO service_whitelist_rev (provider,service,team) VALUES (?,?,?)`, provider, service, team)
		exec(`INSERT INTO service_prefix (prefix,name,service,provider) VALUES (?,?,?,?)`, "s", "synthetic queue service", service, provider)
		for _, mask := range []int64{1, 2, 3, 4, 5, 6, 7} {
			exec(`INSERT INTO service_tag (bucket,tag,name,service,provider) VALUES (?,?,?,?,?)`, int32(201608), mask, "synthetic queue service", service, provider)
		}
	} else if input.Action != "read" {
		t.Fatal("invalid service fixture action")
	}
	count := func(key, statement string, args ...any) {
		t.Helper()
		var value int
		if err := s.Query(statement, args...).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&value); err != nil {
			t.Fatal(err)
		}
		counts[key] += value
	}
	count("service", `SELECT count(*) FROM service WHERE provider=? AND id=?`, provider, service)
	count("whitelist", `SELECT count(*) FROM service_whitelist WHERE team=? AND provider=? AND service=?`, team, provider, service)
	count("whitelist_rev", `SELECT count(*) FROM service_whitelist_rev WHERE provider=? AND service=?`, provider, service)
	count("prefix", `SELECT count(*) FROM service_prefix WHERE prefix=? AND name=? AND service=?`, "s", "synthetic queue service", service)
	for _, mask := range []int64{1, 2, 3, 4, 5, 6, 7} {
		count("tags", `SELECT count(*) FROM service_tag WHERE bucket=? AND tag=? AND name=? AND service=?`, int32(201608), mask, "synthetic queue service", service)
	}
	count("clients", `SELECT count(*) FROM clients WHERE user=?`, user)
	count("prekeys", `SELECT count(*) FROM prekeys WHERE user=?`, user)
	count("mls_public_keys", `SELECT count(*) FROM mls_public_keys WHERE user=?`, user)
	count("properties", `SELECT count(*) FROM properties WHERE user=?`, user)
	if mode == "cassandra" {
		count("bot_association", `SELECT count(*) FROM service_user WHERE provider=? AND service=?`, provider, service)
		count("team_bot_association", `SELECT count(*) FROM service_team WHERE provider=? AND service=?`, provider, service)
		count("user", `SELECT count(*) FROM user WHERE id=?`, user)
		var p, sid gocql.UUID
		if err := s.Query(`SELECT provider,service,status,name,user_type FROM user WHERE id=?`, user).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&p, &sid, &status, &name, &userType); err != nil {
			t.Fatal(err)
		}
		accountProvider, accountService = p.String(), sid.String()
	} else {
		for _, query := range []struct {
			key, statement string
			args           []any
		}{
			{"bot_association", `SELECT count(*) FROM bot_conv WHERE id=$1::uuid`, []any{user.String()}},
			{"team_bot_association", `SELECT count(*) FROM bot_conv WHERE id=$1::uuid AND conv_team=$2::uuid`, []any{user.String(), team.String()}},
			{"user", `SELECT count(*) FROM wire_user WHERE id=$1::uuid`, []any{user.String()}},
		} {
			var count int
			if err := postgres.QueryRow(ctx, query.statement, query.args...).Scan(&count); err != nil {
				t.Fatal(err)
			}
			counts[query.key] = count
		}
		if err := postgres.QueryRow(ctx, `SELECT provider::text,service::text,account_status,name,user_type FROM wire_user WHERE id=$1::uuid`, user.String()).Scan(&accountProvider, &accountService, &status, &name, &userType); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(map[string]any{"counts": counts, "status": status, "user_type": userType, "backend": mode, "provider": accountProvider, "service": accountService, "name": name})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
