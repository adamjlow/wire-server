package cargohold

import (
	"net/http"
	"strings"
)

func (s *Server) legacy(w http.ResponseWriter, r *http.Request, segments []string, user string) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	var key string
	var otr bool
	switch {
	case len(segments) == 2 && segments[0] == "assets":
		if !validUUID(r.URL.Query().Get("conv_id")) {
			w.WriteHeader(400)
			return
		}
		key = segments[1]
	case len(segments) == 4 && segments[0] == "conversations" && segments[2] == "assets":
		if !validUUID(segments[1]) || !validUUID(segments[3]) {
			w.WriteHeader(400)
			return
		}
		key = segments[3]
	case len(segments) == 5 && segments[0] == "conversations" && segments[2] == "otr" && segments[3] == "assets":
		if !validUUID(segments[1]) || !validUUID(segments[4]) {
			w.WriteHeader(400)
			return
		}
		key = "otr/" + strings.ToLower(segments[1]) + "/" + strings.ToLower(segments[4])
		otr = true
	default:
		http.NotFound(w, r)
		return
	}
	meta, err := s.Store.Metadata(r.Context(), key)
	if err != nil {
		serverError(w)
		return
	}
	if (otr && !validUUID(meta["user"])) || (!otr && !strings.Contains(strings.ToLower(meta["zasset"]), "public=true")) {
		assetError(w, 404, "not-found", "Asset not found")
		return
	}
	s.redirect(w, r, key, "")
}
