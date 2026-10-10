// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
)

const scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
const scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
const scimListSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"

type scimError struct {
	Status       int
	Type, Detail string
}

func (e *scimError) Error() string { return "SCIM request failed" }
func scimResponse(w http.ResponseWriter, r *http.Request, status int, body any) {
	contentType := "application/scim+json;charset=utf-8"
	if strings.Contains(r.Header.Get("Accept"), "application/json") && !strings.Contains(r.Header.Get("Accept"), "application/scim+json") {
		contentType = "application/json;charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	encoded, _ := json.Marshal(body)
	_, _ = w.Write(encoded)
}
func scimFailure(w http.ResponseWriter, err error) {
	var e *scimError
	if !errors.As(err, &e) {
		e = &scimError{Status: 500, Detail: "Server Error"}
	}
	body := map[string]any{"schemas": []string{scimErrorSchema}, "status": strconv.Itoa(e.Status)}
	if e.Type != "" {
		body["scimType"] = e.Type
	}
	if e.Detail != "" {
		body["detail"] = e.Detail
	}
	w.Header().Set("Content-Type", "application/scim+json;charset=utf-8")
	w.WriteHeader(e.Status)
	encoded, _ := json.Marshal(body)
	_, _ = w.Write(encoded)
}

type scimTokenLookup interface {
	lookupToken(context.Context, string) (*tokenInfo, error)
}

func scimAuthenticate(ctx context.Context, s scimTokenLookup, r *http.Request) (*tokenInfo, error) {
	values, exists := r.Header["Authorization"]
	if !exists || len(values) == 0 {
		return nil, &scimError{Status: 401, Detail: "authorization failed: Token not provided"}
	}
	value := values[0]
	if !strings.HasPrefix(value, "Bearer ") || !utf8.ValidString(value) {
		return nil, &scimError{Status: 400, Detail: "Invalid Authorization header"}
	}
	info, err := s.lookupToken(ctx, strings.TrimPrefix(value, "Bearer "))
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, &scimError{Status: 401, Detail: "authorization failed: Invalid SCIM token"}
	}
	return info, nil
}

// hscim folds attribute names recursively, leaving string values unchanged.
// Process keys in native map order before folding, retaining its last-key rule.
func scimFoldJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := map[string]any{}
		for _, k := range keys {
			out[cases.Fold().String(k)] = scimFoldJSON(v[k])
		}
		return out
	case []any:
		for i := range v {
			v[i] = scimFoldJSON(v[i])
		}
		return v
	default:
		return value
	}
}
func scimJSON(r *http.Request) (map[string]any, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/json" && contentType != "application/scim+json") {
		return nil, &scimError{Status: 415, Detail: "Unsupported Media Type"}
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid JSON request"}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Invalid JSON request"}
	}
	object, ok := scimFoldJSON(value).(map[string]any)
	if !ok {
		return nil, &scimError{Status: 400, Type: "invalidSyntax", Detail: "Expected JSON object"}
	}
	return object, nil
}
