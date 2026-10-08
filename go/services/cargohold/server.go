package cargohold

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var errNotFound = errors.New("asset not found")
var retentionNames = map[string]string{"1": "eternal", "2": "persistent", "3": "volatile", "4": "eternal-infrequent_access", "5": "expiring"}

type Server struct {
	Config  Config
	Store   Store
	ready   atomic.Bool
	uploads atomic.Uint64
	bytes   atomic.Uint64
}

func NewServer(c Config, s Store) *Server {
	server := &Server{Config: c, Store: s}
	server.ready.Store(true)
	return server
}
func (s *Server) StopAccepting() { s.ready.Store(false) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/i/status" && (r.Method == "GET" || r.Method == "HEAD") {
		if !s.ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(200)
		return
	}
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path == "/i/metrics" && r.Method == "GET" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "net_s3_upload_ok %d\nnet_s3_upload_size %d\n", s.uploads.Load(), s.bytes.Load())
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	segments := strings.Split(path, "/")
	if len(segments) < 2 || (segments[0] != "bot" && segments[0] != "provider") || segments[1] != "assets" {
		http.NotFound(w, r)
		return
	}
	principal := segments[0]
	if r.Header.Get("Z-Type") != principal {
		w.WriteHeader(403)
		return
	}
	owner := r.Header.Get("Z-" + principal)
	if !validUUID(owner) {
		w.WriteHeader(400)
		return
	}
	owner = strings.ToLower(owner)
	if len(segments) == 2 && r.Method == "POST" {
		s.upload(w, r, principal, owner)
		return
	}
	if len(segments) != 3 {
		http.NotFound(w, r)
		return
	}
	key, err := objectKey(segments[2])
	if err != nil {
		assetError(w, 404, "not-found", "Asset not found")
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
		token := r.Header.Get("Asset-Token")
		if _, ok := r.Header["Asset-Token"]; !ok {
			token = r.URL.Query().Get("asset_token")
		}
		if !match && token != meta["token"] {
			assetError(w, 404, "not-found", "Asset not found")
			return
		}
		location, err := s.Store.SignedURL(r.Context(), key, r.Header.Get("Z-Host"))
		if errors.Is(err, errNotFound) {
			assetError(w, 404, "not-found", "Asset not found")
			return
		}
		if err != nil {
			serverError(w)
			return
		}
		w.Header().Set("Location", location)
		w.WriteHeader(302)
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

func (s *Server) upload(w http.ResponseWriter, r *http.Request, principal, owner string) {
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
	if n > s.Config.Settings.MaxTotalBytes {
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
		// Audit serialization/logging is completed in the next full-service slice.
		if len(settings.ConvID) == 0 || settings.Filename == nil || settings.Filetype == nil {
			assetError(w, 400, "missing-audit-metadata", "Missing required asset audit metadata")
			return
		}
		serverError(w)
		return
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
	s.uploads.Add(1)
	s.bytes.Add(uint64(n))
	w.Header().Set("Location", principal+"/assets/"+key)
	w.Header().Set("Content-Type", "application/json")
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "label": label, "message": message})
}
func serverError(w http.ResponseWriter) { assetError(w, 500, "server-error", "Server Error.") }
