// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type accountLookupRequest struct {
	IDs             []gocql.UUID
	Handles, Emails []string
	IncludePending  bool
}

type accountLookup struct {
	users           func(context.Context, []gocql.UUID) ([]storedUser, error)
	resolve         func(context.Context, loginIdentifier) (*gocql.UUID, error)
	invitations     func(context.Context, string) ([]storedInvitation, error)
	enqueueDeletion func(context.Context, gocql.UUID) error
	domain, locale  string
}

func uniqueAccountIDs(ids []gocql.UUID) []gocql.UUID {
	seen := map[gocql.UUID]bool{}
	result := []gocql.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func (s accountLookup) lookup(ctx context.Context, request accountLookupRequest) ([]selfProfile, error) {
	if s.users == nil || s.resolve == nil || s.invitations == nil || s.enqueueDeletion == nil {
		return nil, errors.New("account lookup dependencies unavailable")
	}
	project := func(rows []storedUser) ([]selfProfile, error) {
		result := []selfProfile{}
		for _, row := range rows {
			profile, err := row.selfProfile(s.domain, s.locale)
			if err != nil {
				return nil, err
			}
			result = append(result, profile)
		}
		return result, nil
	}
	// Native handler resolves email lookups before handles/IDs, and only
	// deduplicates within the email arm. Cross-arm duplicates remain observable.
	byEmail := []selfProfile{}
	seenEmailProfiles := map[string]bool{}
	for _, email := range request.Emails {
		id, err := s.resolve(ctx, loginIdentifier{email: &email})
		if err != nil {
			return nil, err
		}
		ids := []gocql.UUID{}
		if id != nil {
			ids = append(ids, *id)
		}
		rows, err := s.users(ctx, ids)
		if err != nil {
			return nil, err
		}
		profiles, err := project(rows)
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			// Compare the projected User, not just its ID: a repeated lookup can
			// observe a changed account and native nubOrd then retains both.
			encoded, err := json.Marshal(profile)
			if err != nil {
				return nil, errors.New("account projection failed")
			}
			if !seenEmailProfiles[string(encoded)] {
				seenEmailProfiles[string(encoded)] = true
				byEmail = append(byEmail, profile)
			}
		}
	}
	ids := []gocql.UUID{}
	for _, handle := range request.Handles {
		id, err := s.resolve(ctx, loginIdentifier{handle: &handle})
		if err != nil {
			return nil, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	}
	ids = uniqueAccountIDs(append(ids, request.IDs...))
	rows, err := s.users(ctx, ids)
	if err != nil {
		return nil, err
	}
	profiles, err := project(rows)
	if err != nil {
		return nil, err
	}
	safe := []selfProfile{}
	deletions := []gocql.UUID{}
	for _, profile := range profiles {
		if profile.Status == "pending-invitation" && profile.Email != nil {
			invitations, err := s.invitations(ctx, *profile.Email)
			if err != nil {
				return nil, err
			}
			if len(invitations) == 0 {
				id, _ := gocql.ParseUUID(profile.ID)
				deletions = append(deletions, id)
				continue
			}
		}
		safe = append(safe, profile)
	}
	// partitionM checks every invitation before any queue write. Preserve that
	// ordering: a later lookup failure must not enqueue earlier deletions.
	for _, id := range deletions {
		if err := s.enqueueDeletion(ctx, id); err != nil {
			return nil, err
		}
	}
	result := []selfProfile{}
	for _, profile := range safe {
		if !request.IncludePending && profile.Status == "pending-invitation" {
			continue
		}
		if profile.Type != "app" && profile.Email == nil && len(profile.SSO) == 0 {
			continue
		}
		if profile.Status != "deleted" {
			result = append(result, profile)
		}
	}
	// Email lookups deliberately bypass identity/pending/GC filters. Only the
	// outer handler's deleted-account filter applies to this arm.
	for _, profile := range byEmail {
		if profile.Status != "deleted" {
			result = append(result, profile)
		}
	}
	return result, nil
}

func parseAccountLookup(query url.Values) (accountLookupRequest, string) {
	var request accountLookupRequest
	list := func(key string) []string {
		if value := query.Get(key); value != "" {
			return strings.Split(value, ",")
		}
		return nil
	}
	for _, value := range list("ids") {
		canonical, valid := tokenUUID(value)
		if !valid {
			return request, "Error parsing query parameter ids failed: invalid UUID"
		}
		id, _ := gocql.ParseUUID(canonical)
		request.IDs = append(request.IDs, id)
	}
	for _, value := range list("handles") {
		if !validLoginHandle(value) {
			return request, "Error parsing query parameter handles failed: invalid handle"
		}
		request.Handles = append(request.Handles, value)
	}
	for _, value := range list("email") {
		email, err := canonicalRegistrationEmail(value)
		if err != nil {
			return request, "Error parsing query parameter email failed: invalid email"
		}
		request.Emails = append(request.Emails, email)
	}
	if values, present := query["includePendingInvitations"]; present {
		if len(values) == 0 {
			return request, "Error parsing query parameter includePendingInvitations failed: invalid boolean"
		}
		switch strings.ToLower(values[0]) {
		case "true":
			request.IncludePending = true
		case "false":
		default:
			return request, "Error parsing query parameter includePendingInvitations failed: invalid boolean"
		}
	}
	return request, ""
}

func (s accountLookup) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	request, message := parseAccountLookup(r.URL.Query())
	if message != "" {
		routeParseError(w, message)
		return
	}
	if len(request.IDs)+len(request.Handles)+len(request.Emails) == 0 {
		authenticationWireError(w, 404, "not-found", "no user keys")
		return
	}
	profiles, err := s.lookup(r.Context(), request)
	if err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return
	}
	readJSON(w, r, profiles)
}
