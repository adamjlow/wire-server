// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type brigGroup struct {
	ID        gocql.UUID   `json:"id"`
	Name      string       `json:"name"`
	Members   []gocql.UUID `json:"members"`
	ManagedBy string       `json:"managedBy"`
	CreatedAt time.Time    `json:"createdAt"`
}

func (g *brigGroup) UnmarshalJSON(data []byte) error {
	var v struct {
		ID        *gocql.UUID   `json:"id"`
		Name      *string       `json:"name"`
		Members   *[]gocql.UUID `json:"members"`
		ManagedBy *string       `json:"managedBy"`
		CreatedAt *time.Time    `json:"createdAt"`
	}
	if json.Unmarshal(data, &v) != nil || v.ID == nil || v.Name == nil || v.Members == nil || v.ManagedBy == nil || v.CreatedAt == nil {
		return errors.New("invalid Brig group response")
	}
	*g = brigGroup{*v.ID, *v.Name, *v.Members, *v.ManagedBy, *v.CreatedAt}
	return nil
}
func scimStoredGroup(c Config, g brigGroup) map[string]any {
	members := []any{}
	for _, uid := range g.Members {
		members = append(members, map[string]string{"value": uid.String(), "type": "User", "$ref": c.SCIMBaseURI + "/Users/" + uid.String()})
	}
	stamp := g.CreatedAt.UTC().Format(time.RFC3339Nano)
	return map[string]any{"schemas": []string{scimGroupSchema}, "id": g.ID.String(), "displayName": g.Name, "members": members, "meta": map[string]any{"resourceType": "Group", "created": stamp, "lastModified": stamp, "version": `W/"v1"`, "location": c.SCIMBaseURI + "/Groups/" + g.ID.String()}}
}

type scimGroupInput struct {
	Name       string
	Members    []gocql.UUID
	rawMembers []string
}

func parseSCIMGroup(object map[string]any) (scimGroupInput, error) {
	var out scimGroupInput
	bad := &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid SCIM group request"}
	name, ok := object["displayname"].(string)
	if !ok {
		return out, bad
	}
	out.Name = name
	if schemas, exists := object["schemas"]; exists {
		values, ok := schemas.([]any)
		if !ok {
			return out, bad
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return out, bad
			}
		}
	}
	members := []any{}
	if raw, exists := object["members"]; exists {
		var ok bool
		members, ok = raw.([]any)
		if !ok {
			return out, bad
		}
	}
	out.Members = []gocql.UUID{}
	for _, member := range members {
		m, ok := member.(map[string]any)
		if !ok {
			return out, bad
		}
		value, ok := m["value"].(string)
		if !ok {
			return out, bad
		}
		if _, ok := m["type"].(string); !ok {
			return out, bad
		}
		if _, ok := m["$ref"].(string); !ok {
			return out, bad
		}
		out.rawMembers = append(out.rawMembers, value)
	}
	return out, nil
}

func (g scimGroupInput) resolvedMembers() (scimGroupInput, error) {
	for _, value := range g.rawMembers {
		id, err := gocql.ParseUUID(value)
		if err != nil {
			return g, &scimError{Status: 400, Type: "invalidValue", Detail: "Invalid group member ID: Invalid UUID"}
		}
		g.Members = append(g.Members, id)
	}
	g.rawMembers = nil
	return g, nil
}
func scimGroupName(name string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 4000 {
		return &scimError{Status: 400, Type: "invalidValue", Detail: "outside range [1, 4000]"}
	}
	return nil
}
func (p peerClient) groupAccounts(ctx context.Context, ids []gocql.UUID, requestID string, creating bool) ([]brigAccount, error) {
	if creating {
		status, body, err := p.request(ctx, p.config.Brig, "POST", "/i/users/accounts-by", requestID, "", map[string]any{"include_pending_invitations": false, "include_users_with_expired_invitations": false, "include_users_without_identity": false, "ids": ids, "handles": []string{}})
		if err != nil {
			return nil, err
		}
		var accounts []brigAccount
		if status < 200 || status >= 300 || json.Unmarshal(body, &accounts) != nil {
			return nil, errors.New("invalid Brig group account response")
		}
		return accounts, nil
	}
	accounts := []brigAccount{}
	// Native getUsers issues chunks of 100 and treats malformed successful JSON
	// as an empty result, subsequently producing its missing-user error.
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		values := []string{}
		for _, id := range ids[start:end] {
			values = append(values, id.String())
		}
		status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/users?ids="+url.QueryEscape(strings.Join(values, ",")), requestID, "", nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, errors.New("Brig group account lookup failed")
		}
		var page []brigAccount
		if json.Unmarshal(body, &page) == nil {
			accounts = append(accounts, page...)
		}
	}
	return accounts, nil
}
func (p peerClient) group(ctx context.Context, team, id gocql.UUID, requestID string) (*brigGroup, error) {
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/user-groups/"+team.String()+"/"+id.String()+"/false", requestID, "", nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, nil
	}
	// The native RPC decodes Maybe UserGroup: 200 null is also absence.
	var group *brigGroup
	if status != 200 || json.Unmarshal(body, &group) != nil {
		return nil, errors.New("invalid Brig group response")
	}
	return group, nil
}
func scimMissingGroup(id gocql.UUID) error {
	return &scimError{Status: 404, Detail: "Group " + id.String() + " not found"}
}
func groupMemberGuard(team gocql.UUID, accounts []brigAccount) error {
	bad := []string{}
	for _, account := range accounts {
		if account.Team == nil || *account.Team != team || account.ManagedBy != "scim" {
			bad = append(bad, account.ID.String())
		}
	}
	if len(bad) != 0 {
		return &scimError{Status: 400, Type: "invalidValue", Detail: `These users do not exist, are not in your team, or not "managed_by" = "scim": ` + strings.Join(bad, ", ")}
	}
	return nil
}
func (p peerClient) createSCIMGroup(ctx context.Context, team gocql.UUID, input scimGroupInput, requestID string) (*brigGroup, error) {
	input, err := input.resolvedMembers()
	if err != nil {
		return nil, err
	}
	accounts, err := p.groupAccounts(ctx, input.Members, requestID, true)
	if err != nil {
		return nil, err
	}
	if err = groupMemberGuard(team, accounts); err != nil {
		return nil, err
	}
	if err = scimGroupName(input.Name); err != nil {
		return nil, err
	}
	status, body, err := p.request(ctx, p.config.Brig, "POST", "/i/user-groups/full", requestID, "", map[string]any{"managed_by": "scim", "team_id": team.String(), "creator_user_id": nil, "new_group": map[string]any{"name": input.Name, "members": input.Members}})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, &scimError{Status: 500, Detail: string(body)}
	}
	var group brigGroup
	if json.Unmarshal(body, &group) != nil {
		return nil, errors.New("invalid Brig group creation response")
	}
	return &group, nil
}
func (p peerClient) updateSCIMGroup(ctx context.Context, team, id gocql.UUID, input scimGroupInput, requestID string) (*brigGroup, error) {
	group, err := p.group(ctx, team, id, requestID)
	if err != nil {
		return nil, err
	}
	if group == nil || group.ManagedBy != "scim" {
		return nil, scimMissingGroup(id)
	}
	if err = scimGroupName(input.Name); err != nil {
		return nil, err
	}
	input, err = input.resolvedMembers()
	if err != nil {
		return nil, err
	}
	current := map[gocql.UUID]bool{}
	for _, uid := range group.Members {
		current[uid] = true
	}
	added := map[gocql.UUID]bool{}
	for _, uid := range input.Members {
		if !current[uid] {
			added[uid] = true
		}
	}
	ids := []gocql.UUID{}
	for uid := range added {
		ids = append(ids, uid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	if len(ids) > 0 {
		accounts, err := p.groupAccounts(ctx, ids, requestID, false)
		if err != nil {
			return nil, err
		}
		if err = groupMemberGuard(team, accounts); err != nil {
			return nil, err
		}
		found := map[gocql.UUID]bool{}
		for _, account := range accounts {
			found[account.ID] = true
		}
		for _, uid := range ids {
			if !found[uid] {
				return nil, &scimError{Status: 404, Detail: "User " + uid.String() + " not found"}
			}
		}
	}
	status, body, err := p.request(ctx, p.config.Brig, "PUT", "/i/user-groups", requestID, "", map[string]any{"team_id": team.String(), "group_id": id.String(), "name": input.Name, "members": input.Members})
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, scimMissingGroup(id)
	}
	if status < 200 || status >= 300 {
		return nil, &scimError{Status: 500, Detail: string(body)}
	}
	group, err = p.group(ctx, team, id, requestID)
	if err == nil && group == nil {
		err = scimMissingGroup(id)
	}
	return group, err
}

// Only this comparison reaches Brig in the native SCIM group interpreter;
// other syntactically valid filters fail rather than broaden the result set.
var scimGroupFilter = regexp.MustCompile(`(?is)^\s*(?:([^\s]+):)?displayName\s+co\s+("(?:[^"\\]|\\.)*")\s*$`)

func (p peerClient) listSCIMGroups(ctx context.Context, team gocql.UUID, query url.Values, requestID string) ([]brigGroup, int, int, error) {
	start, count := 0, -1
	for _, arg := range []string{"startIndex", "count"} {
		if raw, ok := query[arg]; ok && len(raw) > 0 {
			n, err := strconv.Atoi(raw[0])
			if err != nil {
				return nil, 0, 0, &scimError{Status: 400, Detail: "Invalid " + arg}
			}
			if arg == "startIndex" {
				if n > 0 {
					start = n - 1
				}
			} else {
				count = max(n, 0)
			}
		}
	}
	q := url.Values{"managedBy": {"scim"}, "startIndex": {strconv.Itoa(start)}}
	if count >= 0 {
		q.Set("count", strconv.Itoa(count))
	}
	if raw, exists := query["filter"]; exists && len(raw) > 0 {
		match := scimGroupFilter.FindStringSubmatch(raw[0])
		if match == nil {
			return nil, 0, 0, &scimError{Status: 500, Detail: "Unsupported SCIM filter"}
		}
		// The shared native FromHttpApiData Filter parser supports User20 only,
		// even on Groups; its schema URI matching remains case-sensitive.
		if match[1] != "" && match[1] != "urn:ietf:params:scim:schemas:core:2.0:User" {
			return nil, 0, 0, &scimError{Status: 400, Type: "invalidFilter", Detail: "Invalid SCIM filter"}
		}
		var value string
		if json.Unmarshal([]byte(match[2]), &value) != nil {
			return nil, 0, 0, &scimError{Status: 400, Type: "invalidFilter", Detail: "Invalid SCIM filter"}
		}
		q.Set("nameContains", value)
	}
	status, body, err := p.request(ctx, p.config.Brig, "GET", "/i/user-groups/"+team.String()+"?"+q.Encode(), requestID, "", nil)
	if err != nil {
		return nil, 0, 0, err
	}
	var page struct {
		Page  []brigGroup `json:"page"`
		Total int         `json:"total"`
	}
	if status < 200 || status >= 300 || json.Unmarshal(body, &page) != nil {
		return nil, 0, 0, errors.New("invalid Brig group page")
	}
	return page.Page, page.Total, start + 1, nil
}
func serveSCIMGroups(w http.ResponseWriter, r *http.Request, c Config, s scimTokenLookup, p peerClient) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "scim" || parts[1] != "v2" || parts[2] != "Groups" {
		return false
	}
	info, err := scimAuthenticate(r.Context(), s, r)
	if err != nil {
		scimFailure(w, err)
		return true
	}
	var id gocql.UUID
	if len(parts) == 4 {
		id, err = gocql.ParseUUID(parts[3])
		if err != nil {
			scimFailure(w, &scimError{Status: 400, Detail: "Invalid group ID"})
			return true
		}
	}
	ctx, requestID := r.Context(), r.Header.Get("Request-Id")
	var group *brigGroup
	status := 200
	switch r.Method {
	case "GET", "HEAD":
		if len(parts) == 3 {
			groups, total, start, err := p.listSCIMGroups(ctx, info.Team, r.URL.Query(), requestID)
			if err != nil {
				scimFailure(w, err)
				return true
			}
			resources := []any{}
			for _, g := range groups {
				resources = append(resources, scimStoredGroup(c, g))
			}
			scimResponse(w, r, 200, map[string]any{"schemas": []string{scimListSchema}, "totalResults": total, "itemsPerPage": len(resources), "startIndex": start, "Resources": resources})
			return true
		}
		group, err = p.group(ctx, info.Team, id, requestID)
		if err == nil && group == nil {
			err = scimMissingGroup(id)
		}
	case "POST", "PUT":
		if (r.Method == "POST" && len(parts) != 3) || (r.Method == "PUT" && len(parts) != 4) {
			w.WriteHeader(405)
			return true
		}
		var object map[string]any
		object, err = scimJSON(r)
		if err == nil {
			var input scimGroupInput
			input, err = parseSCIMGroup(object)
			if err == nil {
				if r.Method == "POST" {
					status = 201
					group, err = p.createSCIMGroup(ctx, info.Team, input, requestID)
				} else {
					group, err = p.updateSCIMGroup(ctx, info.Team, id, input, requestID)
				}
			}
		}
	case "DELETE":
		if len(parts) != 4 {
			w.WriteHeader(405)
			return true
		}
		var code int
		var body []byte
		code, body, err = p.request(ctx, p.config.Brig, "DELETE", "/i/user-groups/"+info.Team.String()+"/"+id.String()+"/managed/scim", requestID, "", nil)
		if err == nil {
			var upstream apiError
			_ = json.Unmarshal(body, &upstream)
			if code == 403 && upstream.Label == "user-group-managed-by-mismatch" {
				err = &scimError{Status: 403, Detail: "forbidden: Group is not managed by SCIM: " + id.String()}
			} else if code != 404 && (code < 200 || code >= 300) {
				err = errors.New("Brig group deletion failed")
			}
		}
		if err != nil {
			scimFailure(w, err)
		} else {
			w.WriteHeader(204)
		}
		return true
	case "PATCH":
		if len(parts) != 4 {
			w.WriteHeader(405)
			return true
		}
		// The native undefined handler is reached only after servant decodes
		// the PatchOp body. Its exception escapes the inner SCIM/cache wrapper.
		serveUnsupportedGroupPatch(w, r)
		return true
	default:
		w.WriteHeader(405)
		return true
	}
	if err != nil {
		scimFailure(w, err)
	} else {
		scimResponse(w, r, status, scimStoredGroup(c, *group))
	}
	return true
}

// Preserve the native unsupported route without inventing group mutation.
func serveUnsupportedGroupPatch(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (media != "application/scim+json" && media != "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var object map[string]any
	err = decoder.Decode(&object)
	message := "Invalid JSON request"
	if err != nil {
		// Actual pinned Aeson diagnostic for a truncated object opening. Read
		// only this small buffered prefix; never copy an unbounded request body.
		prefix, _ := io.ReadAll(io.LimitReader(decoder.Buffered(), 65))
		if errors.Is(err, io.ErrUnexpectedEOF) && strings.TrimSpace(string(prefix)) == "{" {
			message = "Unexpected end-of-input, expecting record key literal or }"
		}
	} else {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = errors.New("trailing JSON")
		}
		if err == nil {
			_, err = parseSCIMPatch(object)
		}
	}
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, message)
		return
	}
	w.Header().Del("Cache-Control")
	w.Header().Del("Pragma")
	wireError(w, http.StatusInternalServerError, "server-error", "Internal Server Error")
}
