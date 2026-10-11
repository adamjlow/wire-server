// SPDX-License-Identifier: AGPL-3.0-only
package brig

import "net/http"

func (s blocklistStore) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	values, present := r.URL.Query()["email"]
	if !present || len(values) == 0 {
		routeParseError(w, "Query parameter email is required")
		return
	}
	email, err := canonicalRegistrationEmail(values[0])
	if err != nil {
		routeParseError(w, "Error parsing query parameter email failed: Invalid email")
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		found, err := s.exists(r.Context(), email)
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
		} else if found {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(404)
		}
		return
	}
	if err := s.write(r.Context(), email, r.Method == http.MethodDelete); err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return
	}
	w.WriteHeader(200)
}
