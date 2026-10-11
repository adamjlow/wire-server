// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var (
	errClientMissing             = errors.New("client not found")
	errClientCapabilitiesRemoved = errors.New("client capabilities cannot be removed")
	errClientMLSDuplicate        = errors.New("duplicate MLS public key")
)

type clientUpdateRequest struct {
	label      *string
	caps       *[]int32
	prekeys    []uncheckedPrekey
	last       *uncheckedPrekey
	publicKeys map[string][]byte
}

func (clientUpdateRequest) String() string { return "<ClientUpdateRequest>" }

type clientUpdateService struct {
	store      clientUpdateStore
	consumable bool
	setup      func(context.Context, gocql.UUID, string, string) error
}

func (s clientUpdateService) update(ctx context.Context, user gocql.UUID, id string, request clientUpdateRequest, requestID string) error {
	client, err := s.store.get(ctx, user, id)
	if err != nil {
		return err
	}
	if client == nil {
		return errClientMissing
	}
	if request.label != nil {
		if err := s.store.updateLabel(ctx, user, id, *request.label); err != nil {
			return err
		}
	}
	if request.caps != nil {
		caps := map[int32]bool{}
		old := map[int32]bool{}
		for _, cap := range *request.caps {
			caps[cap] = true
		}
		for _, cap := range client.Capabilities {
			old[cap] = true
			if !caps[cap] {
				return errClientCapabilitiesRemoved
			}
		}
		if s.consumable && caps[2] && !old[2] {
			if s.setup == nil {
				return errors.New("notification setup unavailable")
			}
			if err := s.setup(ctx, user, id, requestID); err != nil {
				return err
			}
		}
		if err := s.store.updateCapabilities(ctx, user, id, *request.caps); err != nil {
			return err
		}
	}
	keys := make([]uncheckedPrekey, 0, len(request.prekeys)+1)
	if request.last != nil {
		keys = append(keys, *request.last)
	}
	keys = append(keys, request.prekeys...)
	for _, key := range keys {
		if !checkPrekeyBundle(key) {
			return errPrekeyBundle
		}
	}
	if err := s.store.updatePrekeys(ctx, user, id, keys); err != nil {
		return err
	}
	return s.store.addMLSPublicKeys(ctx, user, id, request.publicKeys)
}

func decodeClientUpdate(reader io.Reader, version int) (clientUpdateRequest, error) {
	invalid := errors.New("invalid client update request")
	var request clientUpdateRequest
	var raw json.RawMessage
	decoder := json.NewDecoder(reader)
	var extra any
	if decoder.Decode(&raw) != nil || decoder.Decode(&extra) != io.EOF {
		return request, invalid
	}
	// Preserve native Aeson first-key JSON objects, including nested maps.
	parsed, err := parsePropertyValue(raw)
	if err != nil {
		return request, invalid
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return request, invalid
	}
	fields, err := registrationObject(canonical)
	if err != nil {
		return request, invalid
	}
	request.label, err = registrationText(fields, "label", false)
	if err != nil {
		return request, invalid
	}
	decodeKey := func(raw json.RawMessage) (uncheckedPrekey, error) {
		var key uncheckedPrekey
		obj, err := registrationObject(raw)
		if err != nil {
			return key, invalid
		}
		id, err := registrationInteger(obj["id"])
		if err != nil || id < 0 || id > 65535 {
			return key, invalid
		}
		key.ID = uint16(id)
		text, err := registrationText(obj, "key", true)
		if err != nil {
			return key, invalid
		}
		key.Key = *text
		return key, nil
	}
	if registrationPresent(fields["prekeys"]) {
		var values []json.RawMessage
		if json.Unmarshal(fields["prekeys"], &values) != nil {
			return request, invalid
		}
		for _, value := range values {
			key, err := decodeKey(value)
			if err != nil {
				return request, err
			}
			request.prekeys = append(request.prekeys, key)
		}
	}
	if registrationPresent(fields["lastkey"]) {
		key, err := decodeKey(fields["lastkey"])
		if err != nil || key.ID != 65535 {
			return request, invalid
		}
		request.last = &key
	}
	if registrationPresent(fields["capabilities"]) {
		var names []string
		if json.Unmarshal(fields["capabilities"], &names) != nil {
			return request, invalid
		}
		set := map[int32]bool{}
		for _, name := range names {
			switch name {
			case "legalhold-implicit-consent":
				set[1] = true
			case "consumable-notifications":
				if version <= 7 {
					return request, invalid
				}
				set[2] = true
			default:
				return request, invalid
			}
		}
		caps := []int32{}
		for _, cap := range []int32{1, 2} {
			if set[cap] {
				caps = append(caps, cap)
			}
		}
		request.caps = &caps
	}
	request.publicKeys = map[string][]byte{}
	if registrationPresent(fields["mls_public_keys"]) {
		keys, err := registrationObject(fields["mls_public_keys"])
		if err != nil {
			return request, invalid
		}
		for scheme, raw := range keys {
			var text string
			if !validSignatureScheme(scheme) || json.Unmarshal(raw, &text) != nil || strings.ContainsAny(text, "\r\n") {
				return request, invalid
			}
			data, err := base64.StdEncoding.Strict().DecodeString(text)
			if err != nil {
				return request, invalid
			}
			request.publicKeys[scheme] = append([]byte{}, data...)
		}
	}
	return request, nil
}

func (s clientUpdateService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if r.Method != http.MethodPut || len(parts) != 2 || parts[0] != "clients" {
		return false
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	id, err := parseClientID(parts[1])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(415)
		return true
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	request, err := decodeClientUpdate(r.Body, version)
	if err != nil {
		routeParseError(w, "Invalid client update request")
		return true
	}
	err = s.update(r.Context(), user, id, request, r.Header.Get("Request-Id"))
	switch {
	case err == nil:
		w.WriteHeader(200)
	case errors.Is(err, errClientMissing):
		authenticationWireError(w, 404, "client-not-found", "Client not found")
	case errors.Is(err, errClientCapabilitiesRemoved):
		authenticationWireError(w, 409, "client-capabilities-cannot-be-removed", "You can only add capabilities to a client, not remove them.")
	case errors.Is(err, errPrekeyBundle):
		authenticationWireError(w, 400, "bad-request", "Malformed prekeys uploaded")
	case errors.Is(err, errClientMLSDuplicate):
		authenticationWireError(w, 400, "mls-duplicate-public-key", "MLS public key for the given signature scheme already exists")
	default:
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
	}
	return true
}
