// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

var errMLSInvalidUser = errors.New("invalid MLS target user")
var errMLSLegalhold = errors.New("MLS target under legalhold")

type mlsBundleEntry struct {
	User   string `json:"user"`
	Domain string `json:"domain"`
	Client string `json:"client"`
	Ref    string `json:"key_package_ref"`
	Data   string `json:"key_package"`
}
type mlsBundle struct {
	Entries []mlsBundleEntry `json:"key_packages"`
}

func (s mlsClientReads) claimOne(ctx context.Context, user gocql.UUID, client string, suite uint16) (*mlsPackageRow, error) {
	select {
	case s.claimGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.claimGate }()
	rows, err := s.usablePackages(ctx, user, client, suite)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(rows))))
	if err != nil {
		return nil, err
	}
	row := rows[n.Int64()]
	if err := s.removeOne(ctx, user, client, suite, row.ref); err != nil {
		return nil, err
	}
	return &row, nil
}
func (s mlsClientReads) claimLocal(ctx context.Context, claimant gocql.UUID, origin string, target gocql.UUID, skip *string, suite uint16, requestID string) (mlsBundle, error) {
	result := mlsBundle{Entries: []mlsBundleEntry{}}
	user, err := s.user(ctx, target)
	if err != nil {
		return result, err
	}
	if user == nil || user.Status == nil || !(*user.Status == 3 || user.Activated && *user.Status == 0) {
		return result, errMLSInvalidUser
	}
	if user.Team != nil {
		status, err := s.legalhold(ctx, target, *user.Team, requestID)
		if err != nil {
			return result, err
		}
		if status == "pending" || status == "enabled" {
			return result, errMLSLegalhold
		}
	}
	clients, err := s.clients(ctx, target)
	if err != nil {
		return result, err
	}
	ids := make([]string, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.ID)
	}
	ids, err = nativeClientIDSet(ids)
	if err != nil {
		return result, err
	}
	if origin == s.domain {
		if err := s.guard(ctx, claimant, map[string][]string{target.String(): ids}, requestID); err != nil {
			return result, err
		}
	}
	for _, client := range ids {
		if origin == s.domain && claimant == target && skip != nil && client == *skip {
			continue
		}
		row, err := s.claimOne(ctx, target, client, suite)
		if err != nil {
			return result, err
		}
		if row != nil {
			result.Entries = append(result.Entries, mlsBundleEntry{target.String(), s.domain, client, base64.StdEncoding.EncodeToString(row.ref), base64.StdEncoding.EncodeToString(row.data)})
		}
	}
	return result, nil
}

func (p brigPeers) mlsUserLegalhold(ctx context.Context, user, team gocql.UUID, requestID string) (string, error) {
	raw, status, err := p.galleyRPCStatus(ctx, http.MethodGet, "/teams/"+team.String()+"/legalhold/"+user.String(), requestID, nil, true, false, &user)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", nil
	}
	var response struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return "", errors.New("invalid legalhold status")
	}
	switch response.Status {
	case "pending", "enabled", "disabled", "no_consent":
		return response.Status, nil
	default:
		return "", errors.New("invalid legalhold status")
	}
}
func (s clientStore) deleteMLSOne(ctx context.Context, user gocql.UUID, client string, suite uint16, ref []byte) error {
	return s.cassandra.Query(`DELETE FROM mls_key_packages WHERE user=? AND client=? AND cipher_suite=? AND ref=?`, user, client, int32(suite), ref).WithContext(ctx).Consistency(gocql.LocalQuorum).RetryPolicy(&gocql.SimpleRetryPolicy{NumRetries: 5}).Exec()
}

func mlsClaimError(w http.ResponseWriter, err error) {
	var validation mlsValidationFailure
	switch {
	case errors.Is(err, errMLSInvalidUser):
		authenticationWireError(w, 400, "invalid-user", "Invalid user")
	case errors.Is(err, errMLSLegalhold):
		authenticationWireError(w, 409, "mls-legal-hold-not-allowed", "A user who is under legal-hold may not participate in MLS conversations")
	case errors.As(err, &validation):
		authenticationWireError(w, 400, validation.label, validation.message)
	default:
		prekeyClaimHTTPError(w, err)
	}
}

func (s mlsClientReads) claimRemote(ctx context.Context, claimant, target gocql.UUID, domain string, suite uint16, requestID string) (mlsBundle, error) {
	result := mlsBundle{Entries: []mlsBundleEntry{}}
	raw, err := s.remote.call(ctx, domain, "claim-key-packages", requestID, map[string]any{"claimant": claimant.String(), "target": target.String(), "cipher_suite": suite})
	if err != nil {
		return result, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return result, errMLSInvalidUser
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	var entries []json.RawMessage
	if string(fields["key_packages"]) == "null" || json.Unmarshal(fields["key_packages"], &entries) != nil {
		return result, errors.New("invalid remote MLS bundle")
	}
	for _, entry := range entries {
		fields, err := registrationObject(entry)
		if err != nil {
			return result, err
		}
		var value mlsBundleEntry
		values := map[string]*string{"user": &value.User, "domain": &value.Domain, "client": &value.Client, "key_package_ref": &value.Ref, "key_package": &value.Data}
		for name, dst := range values {
			text, err := registrationText(fields, name, true)
			if err != nil {
				return result, err
			}
			*dst = *text
		}
		canonical, ok := tokenUUID(value.User)
		if !ok {
			return result, errors.New("invalid remote MLS user")
		}
		value.User = canonical
		value.Domain, err = parseEmailDomain(value.Domain)
		if err != nil {
			return result, err
		}
		value.Client, err = parseClientID(value.Client)
		if err != nil {
			return result, err
		}
		ref, err := base64.StdEncoding.Strict().DecodeString(value.Ref)
		if err != nil {
			return result, err
		}
		data, err := base64.StdEncoding.Strict().DecodeString(value.Data)
		if err != nil {
			return result, err
		}
		value.Ref = base64.StdEncoding.EncodeToString(ref)
		value.Data = base64.StdEncoding.EncodeToString(data)
		result.Entries = append(result.Entries, value)
	}
	// Native Set ordering is qualified user (UUID then domain), numeric client,
	// reference bytes then package bytes; deduplicate identical entries.
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.User != b.User {
			return a.User < b.User
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		if a.Client != b.Client {
			x, _ := strconv.ParseUint(a.Client, 16, 64)
			y, _ := strconv.ParseUint(b.Client, 16, 64)
			return x < y
		}
		ar, _ := base64.StdEncoding.DecodeString(a.Ref)
		br, _ := base64.StdEncoding.DecodeString(b.Ref)
		if !bytes.Equal(ar, br) {
			return bytes.Compare(ar, br) < 0
		}
		ad, _ := base64.StdEncoding.DecodeString(a.Data)
		bd, _ := base64.StdEncoding.DecodeString(b.Data)
		return bytes.Compare(ad, bd) < 0
	})
	deduplicated := result.Entries[:0]
	for _, entry := range result.Entries {
		if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != entry {
			deduplicated = append(deduplicated, entry)
		}
	}
	result.Entries = deduplicated
	for _, value := range result.Entries {
		ref, _ := base64.StdEncoding.Strict().DecodeString(value.Ref)
		data, _ := base64.StdEncoding.Strict().DecodeString(value.Data)
		p, err := decodeUploadedMLSPackage(data)
		if err != nil {
			return result, registrationAdmissionError{Code: 409, Label: "decoding-error", Message: "Key package could not be TLS-decoded"}
		}
		if err := p.validate(value.User + ":" + value.Client + "@" + value.Domain); err != nil {
			return result, err
		}
		if err := p.lifetime.validationError(s.now(), s.maximum); err != nil {
			return result, err
		}
		if value.Domain == s.domain {
			uid, _ := gocql.ParseUUID(value.User)
			key, present, err := s.key(ctx, uid, value.Client, mlsClientScheme(p.suite))
			if err != nil {
				return result, err
			}
			if !present {
				return result, mlsProtocolFailure("No key associated to the given identity and signature scheme")
			}
			if !bytes.Equal(key, p.signatureKey) {
				return result, mlsProtocolFailure("Unrecognised signature key")
			}
		}
		actual := mlsHash(p.suite, append(mlsVector([]byte("MLS 1.0 KeyPackage Reference")), mlsVector(data)...))
		if !bytes.Equal(ref, actual) {
			return result, registrationAdmissionError{Code: 409, Label: "invalid-reference", Message: "Key package's reference does not match its data"}
		}
	}
	return result, nil
}

func (s mlsClientReads) handleClaim(w http.ResponseWriter, r *http.Request, path string) bool {
	federation := path == "/federation/claim-key-packages"
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if !federation && (len(parts) != 5 || parts[0] != "mls" || parts[1] != "key-packages" || parts[2] != "claim") {
		return false
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if !federation && version < 5 {
		return false
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return true
	}
	var claimant, target gocql.UUID
	var origin, domain string
	var skip *string
	suite := uint16(1)
	if federation {
		values := r.Header.Values("Wire-Origin-Domain")
		if len(values) == 0 {
			routeParseError(w, "Header Wire-Origin-Domain is required")
			return true
		}
		var err error
		origin, err = parseEmailDomain(values[0])
		if err != nil {
			routeParseError(w, "Invalid origin domain")
			return true
		}
		domain = s.domain
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err != nil {
			routeParseError(w, "Invalid MLS claim request")
			return true
		}
		fields, err := registrationObject(raw)
		if err != nil {
			routeParseError(w, "Invalid MLS claim request")
			return true
		}
		for name, dst := range map[string]*gocql.UUID{"claimant": &claimant, "target": &target} {
			text, err := registrationText(fields, name, true)
			if err != nil {
				routeParseError(w, "Invalid MLS claim request")
				return true
			}
			canonical, ok := tokenUUID(*text)
			if !ok {
				routeParseError(w, "invalid UUID")
				return true
			}
			*dst, _ = gocql.ParseUUID(canonical)
		}
		n, err := federationInteger(fields["cipher_suite"])
		if err != nil || n < 0 || n > 65535 {
			routeParseError(w, "Invalid MLS claim request")
			return true
		}
		suite = uint16(n)
	} else {
		var ok bool
		claimant, ok = userHeader(w, r)
		if !ok {
			return true
		}
		origin = s.domain
		var err error
		domain, err = parseEmailDomain(parts[3])
		if err != nil {
			routeParseError(w, "invalid domain")
			return true
		}
		canonical, ok := tokenUUID(parts[4])
		if !ok {
			routeParseError(w, "invalid UUID")
			return true
		}
		target, _ = gocql.ParseUUID(canonical)
		if values := r.Header.Values("Z-Client"); len(values) > 0 {
			client, err := parseClientID(values[0])
			if err != nil {
				routeParseError(w, "invalid client ID")
				return true
			}
			skip = &client
		}
		values, present := r.URL.Query()["ciphersuite"]
		if (!present || len(values) == 0) && version >= 8 {
			routeParseError(w, "Query parameter ciphersuite is required")
			return true
		}
		if present && len(values) > 0 {
			var ok bool
			suite, ok = mlsSuiteHex(values[0])
			if !ok {
				routeParseError(w, "Invalid ciphersuite")
				return true
			}
		}
	}
	if !s.enabled {
		if federation {
			readJSON(w, r, nil)
		} else {
			authenticationWireError(w, 400, "mls-not-enabled", "MLS is not configured on this backend. See docs.wire.com for instructions on how to enable it")
		}
		return true
	}
	if mlsClientScheme(suite) == "" {
		authenticationWireError(w, 400, "mls-protocol-error", "Unknown ciphersuite")
		return true
	}
	var bundle mlsBundle
	var err error
	if domain == s.domain {
		bundle, err = s.claimLocal(r.Context(), claimant, origin, target, skip, suite, r.Header.Get("Request-Id"))
	} else {
		bundle, err = s.claimRemote(r.Context(), claimant, target, domain, suite, r.Header.Get("Request-Id"))
	}
	if federation && (errors.Is(err, errMLSInvalidUser) || errors.Is(err, errMLSLegalhold) || errors.Is(err, errPrekeyConsent) || errors.Is(err, errPrekeyOldConsent)) {
		readJSON(w, r, nil)
		return true
	}
	if err != nil {
		var admission registrationAdmissionError
		if errors.As(err, &admission) {
			authenticationWireError(w, admission.Code, admission.Label, admission.Message)
		} else {
			mlsClaimError(w, err)
		}
	} else {
		readJSON(w, r, bundle)
	}
	return true
}
