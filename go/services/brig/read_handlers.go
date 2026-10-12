// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type brigReadHandlers struct {
	config               Config
	users                userStore
	sessions             sessionStore
	peers                brigPeers
	auth                 *reauthenticationHandler
	access               *accessTokenService
	renewal              *accessRenewalHTTP
	login                *loginHTTP
	activation           *activationHTTP
	registration         *registrationHTTP
	passwordReset        *passwordResetHTTP
	passwordChange       *passwordChangeService
	clients              *clientReadHandlers
	index                *searchIndexWriter
	appIDs               func(context.Context, gocql.UUID) ([]string, error)
	accounts             *accountLookup
	bulkClients          *clientBulkReads
	blocklist            *blocklistStore
	federationConfig     *federationConfigRead
	properties           *propertyService
	clientUpdate         *clientUpdateService
	clientDeletion       *clientDeletionService
	clientRegistration   *clientRegistrationService
	clientActivity       *clientActivityService
	prekeys              *prekeyClaimService
	publicClients        *publicClientService
	mlsClients           *mlsClientReads
	connections          *connectionReads
	localConnections     *connectionLocalService
	connectionUpdates    *connectionUpdateService
	remoteConnections    *connectionRemoteService
	publicProfiles       *publicProfileService
	remoteDeletion       *remoteAccountDeletion
	accountDeletion      *accountDeletionHTTP
	providerAccounts     *providerAccountHTTP
	providerAuth         *providerAuthHTTP
	providerReset        *providerResetService
	providerRegistration *providerRegistrationService
	providerDeletion     *providerDeletionService
}

func readJSON(w http.ResponseWriter, r *http.Request, body any) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

func userHeader(w http.ResponseWriter, r *http.Request) (gocql.UUID, bool) {
	// Brig's public router trusts the same gateway-provided Z-User header as
	// native ZLocalUser. Do not silently add a different backend auth policy.
	value, exists := r.Header["Z-User"]
	if !exists || len(value) == 0 {
		routeParseError(w, "Header Z-User is required")
		return gocql.UUID{}, false
	}
	canonical, valid := tokenUUID(value[0])
	if !valid {
		routeParseError(w, "Error parsing header Z-User failed: invalid UUID")
		return gocql.UUID{}, false
	}
	uuid, _ := gocql.ParseUUID(canonical)
	return uuid, true
}

func routeParseError(w http.ResponseWriter, message string) {
	// Default Servant parser errors are literal bodies, without a newline.
	w.Header()["Content-Type"] = nil
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(message))
}

func (b brigReadHandlers) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	if b.providerDeletion != nil && b.providerDeletion.handle(w, r, path) {
		return true
	}
	if b.providerRegistration != nil && b.providerRegistration.handle(w, r, path) {
		return true
	}
	if b.providerReset != nil && b.providerReset.handle(w, r, path) {
		return true
	}
	if b.providerAuth != nil && b.providerAuth.handle(w, r, path) {
		return true
	}
	if b.providerAccounts != nil && b.providerAccounts.handle(w, r, path) {
		return true
	}
	if b.accountDeletion != nil && b.accountDeletion.handle(w, r, path) {
		return true
	}
	if b.publicProfiles != nil && b.publicProfiles.handle(w, r, path) {
		return true
	}
	if b.remoteDeletion != nil && b.remoteDeletion.handle(w, r, path) {
		return true
	}
	if b.remoteConnections != nil && b.remoteConnections.handle(w, r, path) {
		return true
	}
	if b.connectionUpdates != nil && b.connectionUpdates.handle(w, r, path) {
		return true
	}
	if b.localConnections != nil && b.localConnections.handle(w, r, path) {
		return true
	}
	if b.connections != nil && b.connections.handle(w, r, path) {
		return true
	}
	if b.mlsClients != nil && b.mlsClients.handle(w, r, path) {
		return true
	}
	if b.publicClients != nil && b.publicClients.handle(w, r, path) {
		return true
	}
	if b.prekeys != nil && b.prekeys.handle(w, r, path) {
		return true
	}
	if b.clientActivity != nil && b.clientActivity.handle(w, r, path) {
		return true
	}
	if b.clientRegistration != nil && b.clientRegistration.handle(w, r, path) {
		return true
	}
	if b.clientDeletion != nil && b.clientDeletion.handle(w, r, path) {
		return true
	}
	if b.clientUpdate != nil && b.clientUpdate.handle(w, r, path) {
		return true
	}
	if b.properties != nil && b.properties.handle(w, r, path) {
		return true
	}
	if path == "/federation/api-version" {
		federationVersionHTTP(w, r)
		return true
	}
	if path == "/i/federation/remotes" && b.federationConfig != nil {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		result, err := b.federationConfig.read(r.Context())
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
		} else {
			readJSON(w, r, result)
		}
		return true
	}
	if path == "/i/users/blacklist" && b.blocklist != nil {
		b.blocklist.handle(w, r)
		return true
	}
	if path == "/self/password" && b.passwordChange != nil {
		b.passwordChange.handle(w, r)
		return true
	}
	if b.passwordReset != nil && b.passwordReset.handle(w, r, path) {
		return true
	}
	if path == "/register" && b.registration != nil {
		b.registration.handlePublic(w, r)
		return true
	}
	if b.bulkClients != nil && (path == "/i/clients" || path == "/i/clients/full") {
		b.bulkClients.handle(w, r, path == "/i/clients/full")
		return true
	}
	if path == "/i/users" && r.Method == http.MethodPost && b.registration != nil {
		b.registration.handleInternal(w, r)
		return true
	}
	if path == "/i/users" && b.accounts != nil {
		b.accounts.handle(w, r)
		return true
	}
	if path == "/i/users/activation-code" && b.activation != nil {
		b.activation.handleInternalPair(w, r)
		return true
	}
	if path == "/activate" && b.activation != nil {
		b.activation.handle(w, r)
		return true
	}
	if path == "/login" && b.login != nil {
		b.login.handle(w, r)
		return true
	}
	if path == "/access" && b.renewal != nil {
		b.renewal.handle(w, r)
		return true
	}
	if b.clients != nil && b.clients.handle(w, r, path) {
		return true
	}
	if path == "/access/logout" && b.access != nil {
		b.access.logoutHTTP(w, r)
		return true
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 4 && segments[0] == "i" && segments[1] == "teams" && segments[3] == "apps" && b.appIDs != nil {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		canonical, valid := tokenUUID(segments[2])
		if !valid {
			routeParseError(w, "Error parsing URL piece failed: invalid UUID")
			return true
		}
		team, _ := gocql.ParseUUID(canonical)
		ids, err := b.appIDs(r.Context(), team)
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, ids)
		return true
	}
	if len(segments) == 4 && segments[0] == "i" && segments[1] == "teams" && segments[3] == "size" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		canonical, valid := tokenUUID(segments[2])
		if !valid {
			routeParseError(w, "Error parsing URL piece failed: invalid UUID")
			return true
		}
		team, _ := gocql.ParseUUID(canonical)
		size, err := b.index.teamSize(r.Context(), team, r.Header.Get("Request-Id"))
		if err != nil {
			authenticationWireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, size)
		return true
	}
	if len(segments) == 4 && segments[0] == "i" && segments[1] == "users" && segments[3] == "reauthenticate" && b.auth != nil {
		b.auth.handle(w, r, segments[2])
		return true
	}
	if len(segments) == 5 && segments[0] == "i" && segments[1] == "users" && segments[3] == "features" && segments[4] == "conferenceCalling" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		canonical, valid := tokenUUID(segments[2])
		if !valid {
			routeParseError(w, "invalid UUID")
			return true
		}
		user, _ := gocql.ParseUUID(canonical)
		stored, err := b.users.conferenceCalling(r.Context(), user)
		if err != nil {
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		status, err := conferenceCallingStatus(b.config, stored)
		if err != nil {
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, map[string]any{"status": status, "config": map[string]bool{"useSFTForOneToOneCalls": false}, "ttl": "unlimited"})
		return true
	}
	if len(segments) == 4 && segments[0] == "i" && segments[1] == "users" && segments[3] == "status" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(405)
			return true
		}
		user, err := gocql.ParseUUID(segments[2])
		if err != nil {
			http.Error(w, "Invalid UUID", 400)
			return true
		}
		status, err := b.users.status(r.Context(), user)
		if err != nil {
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		if status == nil {
			wireError(w, 404, "not-found", "User not found")
			return true
		}
		if *status < 0 || int(*status) >= len(accountStatusNames) {
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, map[string]any{"status": accountStatusNames[*status]})
		return true
	}
	if path != "/self" && path != "/cookies" {
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
	if path == "/self" {
		record, err := b.users.get(r.Context(), user)
		if err != nil {
			log.Print("Brig self profile: ", err)
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		if record == nil {
			wireError(w, 404, "not-found", "User not found")
			return true
		}
		enabled, err := b.peers.hasE2EIdentity(r.Context(), user.String(), r.Header.Get("Request-Id"))
		if err != nil {
			log.Print("Brig self feature lookup: ", err)
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		if enabled && record.Handle != nil {
			managed := int32(1)
			record.ManagedBy = &managed
		}
		view, err := record.selfProfile(b.config.Settings.Domain, b.config.Settings.DefaultLocale)
		if err != nil {
			log.Print("Brig self projection: ", err)
			wireError(w, 500, "server-error", "Internal Server Error")
			return true
		}
		readJSON(w, r, view)
		return true
	}
	cc, err := b.sessions.list(r.Context(), user)
	if err != nil {
		wireError(w, 500, "server-error", "Internal Server Error")
		return true
	}
	labels := []string{}
	if value, exists := r.URL.Query()["labels"]; exists && len(value) > 0 && value[0] != "" {
		labels = strings.Split(value[0], ",")
	}
	objects := []map[string]any{}
	for _, cookie := range cc {
		if len(labels) > 0 {
			matched := false
			if cookie.Label != nil {
				for _, label := range labels {
					if label == *cookie.Label {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		typ := "session"
		if cookie.Type == 1 {
			typ = "persistent"
		}
		objects = append(objects, map[string]any{"id": cookie.ID, "type": typ, "created": cookie.Created.UTC().Format(time.RFC3339Nano), "expires": cookie.Expires.UTC().Format(time.RFC3339Nano), "label": cookie.Label, "successor": cookie.Successor})
	}
	readJSON(w, r, map[string]any{"cookies": objects})
	return true
}
