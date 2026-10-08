package cargohold

import (
	"net/http"
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.Logger.Event("Error", "request failed")
			serverError(w)
		}
	}()
	if _, present := r.Header["Request-Id"]; !present {
		id, err := newUUID()
		if err != nil {
			serverError(w)
			return
		}
		r.Header.Set("Request-Id", id)
		if r.URL.Path != "/i/status" && r.URL.Path != "/i/metrics" && r.URL.Path != "/api-version" {
			s.Logger.Event("Info", "generated a new request id for local request", logField{"request", id}, logField{"method", r.Method}, logField{"path", r.URL.Path})
		}
	}
	s.serveHTTP(w, r)
}

func (s *Server) Listening(address string) { s.Logger.Event("Info", "Listening on "+address) }
