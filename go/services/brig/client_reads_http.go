// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type clientReadStore interface {
	get(context.Context, gocql.UUID, string) (*storedClient, error)
	list(context.Context, gocql.UUID) ([]storedClient, error)
	prekeyIDs(context.Context, gocql.UUID, string) ([]uint16, error)
}

type clientReadHandlers struct{ store clientReadStore }

func (h clientReadHandlers) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if segments[0] != "clients" || len(segments) > 3 || len(segments) == 3 && segments[2] != "capabilities" && segments[2] != "prekeys" {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	user, ok := userHeader(w, r)
	if !ok {
		return true
	}
	version, _ := strconv.Atoi(r.Header.Get("X-Wire-API-Version"))
	if len(segments) == 1 {
		clients, err := h.store.list(r.Context(), user)
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		objects := []map[string]any{}
		for _, client := range clients {
			object, err := client.wire(version)
			if err != nil {
				authenticationWireError(w, 500, "server-error", "Internal Server Error")
				return true
			}
			objects = append(objects, object)
		}
		readJSON(w, r, objects)
		return true
	}
	id, err := parseClientID(segments[1])
	if err != nil {
		routeParseError(w, "invalid client ID")
		return true
	}
	if len(segments) == 3 && segments[2] == "prekeys" {
		ids, err := h.store.prekeyIDs(r.Context(), user, id)
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, ids)
		return true
	}
	client, err := h.store.get(r.Context(), user, id)
	if err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return true
	}
	if client == nil {
		if len(segments) == 3 {
			authenticationWireError(w, 404, "client-not-found", "Client not found")
		} else {
			// Native Maybe Client uses an empty text/plain response for Nothing.
			w.Header().Set("Content-Type", "text/plain;charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
		}
		return true
	}
	if len(segments) == 3 {
		readJSON(w, r, clientCapabilities(client.Capabilities, version))
		return true
	}
	object, err := client.wire(version)
	if err != nil {
		authenticationWireError(w, 500, "server-error", "Internal Server Error")
		return true
	}
	readJSON(w, r, object)
	return true
}
