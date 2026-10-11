// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/jackc/pgx/v5"
	"net/http"
	"sync"
	"time"
)

type publicAppInfo struct {
	Category    string `json:"category"`
	Description string `json:"description"`
}
type publicUserProfile struct {
	QualifiedID qualifiedUserID   `json:"qualified_id"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	TextStatus  *string           `json:"text_status,omitempty"`
	Picture     []json.RawMessage `json:"picture"`
	Assets      []profileAsset    `json:"assets"`
	Accent      int32             `json:"accent_id"`
	Deleted     bool              `json:"deleted,omitempty"`
	Service     *serviceReference `json:"service,omitempty"`
	Handle      *string           `json:"handle,omitempty"`
	Expires     *string           `json:"expires_at,omitempty"`
	Team        *string           `json:"team,omitempty"`
	Email       *string           `json:"email,omitempty"`
	Legalhold   string            `json:"legalhold_status"`
	Protocols   []string          `json:"supported_protocols"`
	Type        string            `json:"type"`
	App         *publicAppInfo    `json:"app,omitempty"`
	Searchable  bool              `json:"searchable"`
	Contact     *struct {
		State string `json:"state"`
	} `json:"contact_status,omitempty"`
}

var errPublicProfileAppOwner = errors.New("app profile owner not found")
var errPublicProfileAppPermission = errors.New("app profile no permission")
var errPublicProfileAppAbsent = errors.New("app profile not found")

type publicProfileService struct {
	domain, locale, emailVisibility string
	get                             func(context.Context, gocql.UUID) (*storedUser, error)
	legalhold                       func(context.Context, *storedUser, string) (string, error)
	app                             func(context.Context, gocql.UUID, gocql.UUID, string) (*publicAppInfo, error)
	enqueueDeletion                 func(context.Context, gocql.UUID) error
	now                             func() time.Time
}

func (s publicProfileService) one(ctx context.Context, id gocql.UUID, requestID string) (*publicUserProfile, error) {
	user, err := s.get(ctx, id)
	if err != nil || user == nil {
		return nil, err
	}
	if user.Status != nil && *user.Status == 4 {
		return nil, nil
	}
	lh, err := s.legalhold(ctx, user, requestID)
	if err != nil {
		return nil, err
	}
	profile, err := user.selfProfile(s.domain, s.locale)
	if err != nil {
		return nil, err
	}
	out := publicUserProfile{QualifiedID: profile.QualifiedID, ID: profile.ID, Name: profile.Name, TextStatus: profile.TextStatus, Picture: []json.RawMessage{}, Assets: profile.Assets, Accent: profile.Accent, Deleted: profile.Deleted, Service: profile.Service, Handle: profile.Handle, Expires: profile.Expires, Team: profile.Team, Legalhold: lh, Protocols: profile.Protocols, Type: profile.Type, Searchable: profile.Searchable}
	switch s.emailVisibility {
	case "visible_if_on_team":
		if user.Team != nil {
			out.Email = profile.Email
		}
	case "visible_to_self", "visible_if_on_same_team":
	default:
		return nil, errors.New("invalid profile email visibility")
	}
	if profile.Expires != nil && user.Expires != nil && user.Expires.Before(s.now()) {
		if err = s.enqueueDeletion(ctx, id); err != nil {
			return nil, err
		}
	}
	return &out, nil
}
func (s publicProfileService) list(ctx context.Context, ids []gocql.UUID, requestID string) ([]publicUserProfile, error) {
	results := make([]*publicUserProfile, len(ids))
	errs := make([]error, len(ids))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(8, len(ids)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				results[i], errs[i] = s.one(ctx, ids[i], requestID)
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	out := []publicUserProfile{}
	for i, p := range results {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if p == nil {
			continue
		}
		if !p.Deleted && p.Type == "app" && p.Team != nil {
			tid, _ := gocql.ParseUUID(*p.Team)
			app, err := s.app(ctx, ids[i], tid, requestID)
			if err != nil {
				return nil, err
			}
			p.App = app
		}
		out = append(out, *p)
	}
	return out, nil
}
func (s userAuthStore) publicApp(ctx context.Context, user, team gocql.UUID) (*publicAppInfo, error) {
	var info publicAppInfo
	err := s.withPostgres(ctx, func(conn *pgx.Conn) error {
		var id, tid, creator string
		var metadata []byte
		err := conn.QueryRow(ctx, `SELECT user_id::text,team_id::text,metadata,category,description,creator::text FROM apps WHERE user_id=$1::uuid AND team_id=$2::uuid`, user.String(), team.String()).Scan(&id, &tid, &metadata, &info.Category, &info.Description, &creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return errPublicProfileAppAbsent
		}
		if err != nil {
			return userStorageError(err)
		}
		return validateAppIDRow(id, tid, metadata, info.Category, info.Description, creator)
	})
	return &info, err
}
func (s publicProfileService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "/federation/get-users-by-ids" {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	if _, err := parseEmailDomain(r.Header.Get("Wire-Origin-Domain")); err != nil {
		routeParseError(w, "Invalid federation origin domain")
		return true
	}
	if !publicClientContentType(w, r) {
		return true
	}
	raw, err := publicClientJSON(r)
	if err != nil {
		routeParseError(w, "Invalid federation user request")
		return true
	}
	var text []string
	if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
		routeParseError(w, "Invalid federation user request")
		return true
	}
	ids := make([]gocql.UUID, len(text))
	for i, value := range text {
		canonical, ok := tokenUUID(value)
		if !ok {
			routeParseError(w, "invalid UUID")
			return true
		}
		ids[i], _ = gocql.ParseUUID(canonical)
	}
	profiles, err := s.list(r.Context(), ids, r.Header.Get("Request-Id"))
	switch {
	case errors.Is(err, errPublicProfileAppOwner):
		authenticationWireError(w, 403, "create-app-no-user", "App owner not found")
	case errors.Is(err, errPublicProfileAppPermission):
		authenticationWireError(w, 403, "app-no-permission", "User does not have permission to create or manage apps")
	case errors.Is(err, errPublicProfileAppAbsent):
		authenticationWireError(w, 404, "app-not-found", "App not found")
	case err != nil:
		authenticationHTTPError(w, err)
	default:
		readJSON(w, r, profiles)
	}
	return true
}
