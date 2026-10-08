package cargohold

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var errNotFound = errors.New("asset not found")
var retentionNames = map[string]string{"1": "eternal", "2": "persistent", "3": "volatile", "4": "eternal-infrequent_access", "5": "expiring"}

type Server struct {
	Config           Config
	Store            Store
	Logger           *Logger
	peerClient       *http.Client
	federationClient *http.Client
	ready            atomic.Bool
	uploads          atomic.Uint64
	bytes            atomic.Uint64
	connections      atomic.Int64
	metrics          httpMetrics
}

func NewServer(c Config, s Store) *Server {
	server := &Server{Config: c, Store: s, Logger: NewLogger(c, os.Stdout), peerClient: &http.Client{Timeout: 10 * time.Second}, federationClient: newFederationClient()}
	server.ready.Store(true)
	return server
}
func (s *Server) StopAccepting() { s.ready.Store(false) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	version := 0
	if len(segments) > 0 && looksLikeVersion(segments[0]) {
		text := segments[0]
		if len(segments) > 1 && (segments[1] == "i" || segments[1] == "api-internal") {
			assetError(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
			return
		}
		var err error
		version, err = strconv.Atoi(strings.TrimPrefix(text, "v"))
		if err != nil || text != "v"+strconv.Itoa(version) || version > 19 || s.versionDisabled(version) {
			assetError(w, 404, "unsupported-version", "Version "+text+" is not supported")
			return
		}
		segments = segments[1:]
	}
	r.Header.Del("Z-Api-Version")
	if len(segments) == 2 && segments[0] == "i" && segments[1] == "status" && (r.Method == "GET" || r.Method == "HEAD") {
		if !s.ready.Load() {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		return
	}
	if !s.ready.Load() {
		w.WriteHeader(503)
		return
	}
	if len(segments) == 2 && segments[0] == "i" && segments[1] == "metrics" && r.Method == "GET" {
		s.writeMetrics(w, r)
		return
	}
	if len(segments) == 2 && segments[0] == "federation" {
		s.federation(w, r, segments[1])
		return
	}
	if len(segments) == 3 && segments[0] == "i" && segments[1] == "assets" && r.Method == "GET" {
		key, err := objectKey(segments[2])
		if err != nil {
			w.WriteHeader(400)
			return
		}
		location, err := s.Store.SignedURL(r.Context(), key, "")
		if err != nil {
			s.storeError(w, err)
			return
		}
		meta, _ := s.Store.Metadata(r.Context(), key)
		s.audit(r, "download-url-creation", "", "", "", meta, "", location, "")
		writeJSON(w, 200, location)
		return
	}
	if len(segments) == 0 {
		http.NotFound(w, r)
		return
	}
	principal := "user"
	switch segments[0] {
	case "bot", "provider":
		principal = segments[0]
		if r.Header.Get("Z-Type") != principal {
			w.WriteHeader(403)
			return
		}
		segments = segments[1:]
	case "assets", "conversations":
	default:
		http.NotFound(w, r)
		return
	}
	owner := r.Header.Get("Z-" + principal)
	if !validUUID(owner) {
		w.WriteHeader(400)
		return
	}
	owner = strings.ToLower(owner)
	if principal == "user" && version < 2 && (segments[0] == "conversations" || (segments[0] == "assets" && len(segments) == 2 && validUUID(segments[1]))) {
		s.legacy(w, r, segments, owner)
		return
	}
	if segments[0] != "assets" {
		http.NotFound(w, r)
		return
	}
	segments = segments[1:]
	qualified := false
	if principal == "user" {
		if version < 2 {
			if len(segments) < 1 || (segments[0] != "v3" && segments[0] != "v4") {
				http.NotFound(w, r)
				return
			}
			qualified = segments[0] == "v4"
			segments = segments[1:]
		} else {
			qualified = true
		}
	}
	if len(segments) == 0 && r.Method == "POST" && !(principal == "user" && version < 2 && qualified) {
		maxBytes := s.Config.Settings.MaxTotalBytes
		if principal == "user" {
			var err error
			maxBytes, err = s.userLimit(r, owner)
			if err != nil {
				if errors.Is(err, errUnverified) {
					assetError(w, 403, "unverified-user", "Unverified user")
				} else {
					assetError(w, 403, "not-found", "User not found")
				}
				return
			}
		}
		s.upload(w, r, principal, owner, maxBytes)
		return
	}
	// Token paths remain unqualified even in API v2+.
	if principal == "user" && len(segments) == 2 && segments[1] == "token" {
		s.changeToken(w, r, segments[0], owner)
		return
	}
	domain := s.Config.Settings.FederationDomain
	if qualified {
		if len(segments) != 2 {
			http.NotFound(w, r)
			return
		}
		domain = strings.ToLower(segments[0])
		segments = segments[1:]
	}
	if len(segments) != 1 {
		http.NotFound(w, r)
		return
	}
	if domain != s.Config.Settings.FederationDomain {
		if r.Method == "DELETE" {
			assetError(w, 422, "federation-not-implemented", "Federation is not yet implemented for this endpoint")
			return
		}
		if r.Method == "GET" {
			s.remoteDownload(w, r, domain, segments[0], owner)
			return
		}
		http.NotFound(w, r)
		return
	}
	key, err := objectKey(segments[0])
	if err != nil {
		bareError(w, 400, "Failed reading: takeWhile1")
		return
	}
	if r.Method == "GET" && !validateRequestToken(w, r) {
		return
	}
	meta, err := s.Store.Metadata(r.Context(), key)
	if err != nil {
		serverError(w)
		return
	}
	storedKind, storedOwner := metadataOwner(meta)
	if storedOwner == "" {
		assetError(w, 404, "not-found", "Asset not found")
		return
	}
	match := storedKind == principal && storedOwner == owner
	switch r.Method {
	case "GET":
		token := requestToken(r)
		if !match && !tokenMatches(meta, token) {
			assetError(w, 404, "not-found", "Asset not found")
			return
		}
		s.redirect(w, r, key, r.Header.Get("Z-Host"), meta)
	case "DELETE":
		if !match {
			assetError(w, 403, "unauthorised", "Unauthorised operation")
			return
		}
		if err = s.Store.Delete(r.Context(), key); err != nil {
			serverError(w)
			return
		}
		w.WriteHeader(200)
	default:
		http.NotFound(w, r)
	}
}

func looksLikeVersion(s string) bool {
	if !strings.HasPrefix(s, "v") {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func (s *Server) versionDisabled(v int) bool {
	for _, expr := range s.Config.Settings.DisabledAPIVersions {
		if expr == "v"+strconv.Itoa(v) || (expr == "development" && v == 19) {
			return true
		}
	}
	return false
}
func requestToken(r *http.Request) *string {
	if values, ok := r.Header["Asset-Token"]; ok && len(values) > 0 {
		return &values[0]
	}
	if values, ok := r.URL.Query()["asset_token"]; ok && len(values) > 0 {
		return &values[0]
	}
	return nil
}
func tokenMatches(meta map[string]string, token *string) bool {
	expected, present := meta["token"]
	if token == nil {
		return !present
	}
	return present && expected == *token
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) storeError(w http.ResponseWriter, err error) {
	// Signing/endpoint exceptions use the runtime error serializer, whereas
	// an absent GET asset uses the API response serializer with a charset.
	if errors.Is(err, errNotFound) {
		assetErrorWithType(w, 404, "not-found", "Asset not found", "application/json")
	} else {
		serverError(w)
	}
}
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, key, host string, meta map[string]string) {
	location, err := s.Store.SignedURL(r.Context(), key, host)
	if err != nil {
		s.storeError(w, err)
		return
	}
	kind, owner := requestPrincipal(r)
	s.audit(r, "download-url-creation", kind, owner, s.Config.Settings.FederationDomain, meta, "", location, "")
	w.Header().Set("Location", location)
	w.WriteHeader(302)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, principal, owner string, maxBytes int64) {
	typ := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	if strings.TrimSpace(typ) != "multipart/mixed" {
		w.WriteHeader(415)
		return
	}
	reader, settings, n, err := parseUpload(r.Body)
	if err != nil {
		assetError(w, 400, "client-error", err.Error())
		return
	}
	if n <= 0 {
		assetError(w, 400, "invalid-length", "Invalid content length")
		return
	}
	if n > maxBytes {
		assetError(w, 413, "client-error", "Asset too large")
		return
	}
	ret := settings.Retention
	if ret == "" {
		ret = "persistent"
	}
	var number string
	for id, name := range retentionNames {
		if name == ret {
			number = id
		}
	}
	if number == "" {
		assetError(w, 400, "client-error", "Invalid asset retention")
		return
	}
	meta := map[string]string{principal: owner}
	if s.Config.Settings.AssetAuditLogEnabled {
		if (len(settings.ConvID) == 0 || string(settings.ConvID) == "null") || settings.Filename == nil || settings.Filetype == nil {
			assetError(w, 400, "missing-audit-metadata", "Missing required asset audit metadata")
			return
		}
		var conversation map[string]string
		if err := json.Unmarshal(settings.ConvID, &conversation); err != nil {
			assetError(w, 400, "client-error", "Invalid asset audit metadata")
			return
		}
		audit, err := json.Marshal(map[string]any{"convId": map[string]string{"domain": conversation["domain"], "id": conversation["id"]}, "filename": *settings.Filename, "filetype": *settings.Filetype})
		if err != nil {
			assetError(w, 400, "client-error", "Invalid asset audit metadata")
			return
		}
		meta["wire-metadata"] = percentEncode(audit)
	}
	id, err := newUUID()
	if err != nil {
		serverError(w)
		return
	}
	key := "3-" + number + "-" + id
	response := map[string]any{"key": key, "domain": s.Config.Settings.FederationDomain}
	if !settings.Public {
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			serverError(w)
			return
		}
		token := base64.URLEncoding.EncodeToString(b)
		meta["token"] = token
		response["token"] = token
	}
	seen, err := s.Store.Upload(r.Context(), "v3/"+ret+"/"+id, meta, reader, n)
	if err != nil {
		serverError(w)
		return
	}
	// Reference checks incomplete length after S3 completion: retain that effect.
	if seen != n {
		assetError(w, 400, "incomplete-body", "HTTP content-length header does not match body size")
		return
	}
	if number == "3" || number == "5" {
		days := 28
		if number == "5" {
			days = 365
		}
		response["expires"] = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	}
	s.audit(r, "file-upload", principal, owner, s.Config.Settings.FederationDomain, meta, "upload.path", "v3/"+ret+"/"+id, "")
	s.uploads.Add(1)
	s.bytes.Add(uint64(n))
	location := principal + "/assets/" + key
	if principal == "user" {
		location = "assets/v4/" + s.Config.Settings.FederationDomain + "/" + key
	}
	w.Header().Set("Location", location)
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(response)
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func metadataOwner(meta map[string]string) (string, string) {
	for _, kind := range []string{"user", "bot", "provider"} {
		if validUUID(meta[kind]) {
			return kind, strings.ToLower(meta[kind])
		}
	}
	return "", ""
}
func assetError(w http.ResponseWriter, code int, label, message string) {
	ct := "application/json"
	if wrapped, ok := w.(*responseContext); ok && wrapped.method == "GET" && code == 404 && label == "not-found" {
		ct = "application/json;charset=utf-8"
	}
	assetErrorWithType(w, code, label, message, ct)
}
func assetErrorWithType(w http.ResponseWriter, code int, label, message, ct string) {
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "label": label, "message": message})
}
func serverError(w http.ResponseWriter) { assetError(w, 500, "server-error", "Internal Server Error") }

func randToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
func percentEncode(data []byte) string {
	var out strings.Builder
	for _, b := range data {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || strings.ContainsRune("-_.~:@&=+$,", rune(b)) {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func validToken(token string) bool {
	for _, c := range token {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '=' {
			continue
		}
		return false
	}
	return true
}

func validateRequestToken(w http.ResponseWriter, r *http.Request) bool {
	if token := requestToken(r); token != nil && !validToken(*token) {
		field := "header Asset-Token"
		if _, present := r.Header["Asset-Token"]; !present {
			field = "query parameter asset_token"
		}
		bareError(w, 400, "Error parsing "+field+" failed: Failed reading: Invalid url-safe base-64 characters: "+*token)
		return false
	}
	return true
}
