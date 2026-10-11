//go:build brig_storage_qualification

// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func qualifyDeletionGroupStores(t *testing.T, ctx context.Context, stores map[string]userStore) {
	t.Helper()
	s := stores["postgresql"].userAuthStore
	user, otherUser, team, otherTeam := gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID(), gocql.TimeUUID()
	created := time.Now().UTC().Truncate(time.Millisecond)
	for i := 0; i < 18; i++ {
		id := gocql.TimeUUID()
		tid := team
		if i == 17 {
			tid = otherTeam
		}
		if _, err := s.postgres.Exec(ctx, `INSERT INTO user_group(team_id,id,name,managed_by,created_at) VALUES($1::uuid,$2::uuid,$3,0,$4)`, tid.String(), id.String(), "Synthetic group", created); err != nil {
			t.Fatal(err)
		}
		for _, uid := range []gocql.UUID{user, otherUser} {
			if _, err := s.postgres.Exec(ctx, `INSERT INTO user_group_member(user_group_id,user_id) VALUES($1::uuid,$2::uuid)`, id.String(), uid.String()); err != nil {
				t.Fatal(err)
			}
		}
	}
	first, err := s.deletionGroupPage(ctx, team, nil)
	if err != nil || len(first) != 15 {
		t.Fatal("native first page", len(first), err)
	}
	second, err := s.deletionGroupPage(ctx, team, &first[14])
	if err != nil || len(second) != 2 {
		t.Fatal("native next page", len(second), err)
	}
	if first[14].created != created || first[14].id.String() <= second[0].id.String() {
		t.Fatal("descending native cursor changed")
	}
	for _, mode := range []string{"cassandra", "postgresql", "migration-to-postgresql"} {
		groups, err := stores[mode].userAuthStore.deletionGroupPage(ctx, team, nil)
		if err != nil || len(groups) != 15 {
			t.Fatal("groups follow user selector", mode, err)
		}
	}
	pages := 0
	service := accountDeletionGroups{
		member: func(context.Context, gocql.UUID, gocql.UUID, string) (bool, error) { return true, nil },
		page: func(c context.Context, tid gocql.UUID, cursor *deletionGroupCursor) ([]deletionGroupCursor, error) {
			pages++
			return s.deletionGroupPage(c, tid, cursor)
		},
		remove: s.removeDeletionGroupMember,
	}
	if err := service.execute(ctx, user, team, "synthetic-delete"); err != nil || pages != 3 {
		t.Fatal("native multi-page removal", pages, err)
	}
	var target, other, outsider int64
	if err := s.postgres.QueryRow(ctx, `SELECT count(*) FILTER(WHERE ug.team_id=$1::uuid AND ugm.user_id=$2::uuid),count(*) FILTER(WHERE ug.team_id=$1::uuid AND ugm.user_id=$3::uuid),count(*) FILTER(WHERE ug.team_id=$4::uuid AND ugm.user_id=$2::uuid) FROM user_group_member ugm JOIN user_group ug ON ug.id=ugm.user_group_id`, team.String(), user.String(), otherUser.String(), otherTeam.String()).Scan(&target, &other, &outsider); err != nil || target != 0 || other != 17 || outsider != 1 {
		t.Fatal("membership scope changed", target, other, outsider, err)
	}
	if _, err := s.postgres.Exec(ctx, `UPDATE user_group SET name='' WHERE id=$1::uuid`, first[0].id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.deletionGroupPage(ctx, team, nil); err == nil {
		t.Fatal("invalid native group metadata accepted")
	}
}
