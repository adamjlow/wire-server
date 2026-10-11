// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

// Native activity is deliberately coarse: round up to the next epoch week,
// including when the current second is already a week boundary.
func clientActivityTime(now time.Time) time.Time {
	const week int64 = 604800
	seconds := now.Unix()
	remainder := seconds % week
	if remainder < 0 {
		remainder += week
	}
	return time.Unix(seconds+week-remainder, 0).UTC()
}

func (s clientStore) updateLastActive(ctx context.Context, user gocql.UUID, id string, at time.Time) error {
	// IF EXISTS prevents a missing/deleted client from being resurrected.
	// Native trans ignores the not-applied result; storage failures propagate.
	return s.mutation(ctx, `UPDATE clients SET last_active=? WHERE user=? AND client=? IF EXISTS`, at, user, id)
}

type clientActivityService struct {
	update func(context.Context, gocql.UUID, string, time.Time) error
	now    func() time.Time
}

func (s clientActivityService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if r.Method != http.MethodPost || len(parts) != 5 || parts[0] != "i" || parts[1] != "clients" || parts[4] != "activity" {
		return false
	}
	canonical, valid := tokenUUID(parts[2])
	if !valid {
		routeParseError(w, "invalid UUID")
		return true
	}
	user, _ := gocql.ParseUUID(canonical)
	id, err := parseClientID(parts[3])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	if err := s.update(r.Context(), user, id, clientActivityTime(s.now())); err != nil {
		authenticationHTTPError(w, err)
		return true
	}
	w.WriteHeader(http.StatusOK)
	return true
}
