package cargohold

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
)

var errUnverified = errors.New("unverified user")

func (s *Server) userLimit(r *http.Request, owner string) (int64, error) {
	body, _ := json.Marshal(map[string]any{"include_pending_invitations": false, "include_users_with_expired_invitations": false, "include_users_without_identity": true, "ids": []string{owner}, "handles": []string{}})
	endpoint := "http://" + net.JoinHostPort(s.Config.Brig.Host, strconv.Itoa(s.Config.Brig.Port)) + "/i/users/accounts-by"
	req, err := http.NewRequestWithContext(r.Context(), "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, errNotFound
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Request-Id", r.Header.Get("Request-Id"))
	response, err := s.peerClient.Do(req)
	if err != nil {
		return 0, errNotFound
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, response.Body)
		return 0, errNotFound
	}
	var users []struct {
		Status string  `json:"status"`
		Team   *string `json:"team"`
	}
	if json.NewDecoder(response.Body).Decode(&users) != nil || len(users) == 0 {
		return 0, errNotFound
	}
	if users[0].Status == "active" {
		if users[0].Team != nil {
			return s.Config.Settings.MaxTotalBytes, nil
		}
		return s.Config.Settings.MaxTotalBytesStrict, nil
	}
	if users[0].Status == "ephemeral" {
		return s.Config.Settings.MaxTotalBytesStrict, nil
	}
	return 0, errUnverified
}

func (s *Server) changeToken(w http.ResponseWriter, r *http.Request, asset, owner string) {
	if r.Method != "POST" && r.Method != "DELETE" {
		http.NotFound(w, r)
		return
	}
	key, err := objectKey(asset)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	meta, err := s.Store.Metadata(r.Context(), key)
	if err != nil {
		serverError(w)
		return
	}
	kind, id := metadataOwner(meta)
	if id == "" {
		assetError(w, 404, "not-found", "Asset not found")
		return
	}
	if kind != "user" || id != owner {
		assetError(w, 403, "unauthorised", "Unauthorised operation")
		return
	}
	// Reconstruct typed metadata exactly as the reference copy operation does.
	// Old raw JSON audit metadata is canonicalized and unknown fields are dropped.
	copyMeta := map[string]string{kind: id}
	if audit := parseAuditMetadata(meta["wire-metadata"]); audit != nil {
		encoded, err := json.Marshal(audit)
		if err != nil {
			serverError(w)
			return
		}
		copyMeta["wire-metadata"] = percentEncode(encoded)
	}
	meta = copyMeta
	var token string
	if r.Method == "POST" {
		token, err = randToken()
		if err != nil {
			serverError(w)
			return
		}
		meta["token"] = token
	} else {
		delete(meta, "token")
	}
	if err = s.Store.UpdateMetadata(r.Context(), key, meta); err != nil {
		serverError(w)
		return
	}
	if r.Method == "POST" {
		writeJSON(w, 200, map[string]string{"token": token})
	} else {
		w.WriteHeader(200)
	}
}
