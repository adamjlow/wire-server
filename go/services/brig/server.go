// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func wireError(w http.ResponseWriter, code int, label, message string) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "label": label, "message": message})
}

func disabledVersion(c Config, v int) bool {
	for _, expression := range c.Settings.DisabledVersions {
		if expression == "v"+strconv.Itoa(v) || expression == "development" && v == 19 {
			return true
		}
	}
	return false
}

// This initial handler implements liveness/version and version middleware.
// Business API families and documentation are still under construction. No
// successful fallback response substitutes for a missing business handler.
func NewHandler(c Config) http.Handler {
	return newHandler(c, nil)
}

func newHandler(c Config, business *brigReadHandlers) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		path := r.URL.Path
		segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
		first := segments[0]
		looksVersion := strings.HasPrefix(first, "v")
		for _, ch := range strings.TrimPrefix(first, "v") {
			if ch < '0' || ch > '9' {
				looksVersion = false
			}
		}
		if looksVersion {
			r.Header.Del("X-Wire-API-Version")
			if len(segments) > 1 && (segments[1] == "i" || segments[1] == "api-internal") {
				wireError(w, 404, "unsupported-version", "Internal APIs (`/i/...`) are not under version control")
				return
			}
			v, err := strconv.Atoi(first[1:])
			path = "/" + strings.Join(segments[1:], "/")
			isDocs := strings.HasPrefix(path, "/api/swagger-ui/") || path == "/api/swagger.json" || strings.HasPrefix(path, "/api/swagger.json/")
			if err != nil || first != "v"+strconv.Itoa(v) || v < 0 || v > 19 || disabledVersion(c, v) && !isDocs {
				wireError(w, 404, "unsupported-version", "Version "+first+" is not supported")
				return
			}
			r.Header.Set("X-Wire-API-Version", strconv.Itoa(v))
		}
		if strings.HasPrefix(path, "/i/users/handles") && (path == "/i/users/handles" || strings.HasPrefix(path, "/i/users/handles/")) {
			path = "/i/handles" + strings.TrimPrefix(path, "/i/users/handles")
		}
		if _, present := r.Header["Request-Id"]; !present {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err != nil {
				wireError(w, 500, "server-error", "Internal Server Error")
				return
			}
			raw[6] = (raw[6] & 15) | 64
			raw[8] = (raw[8] & 63) | 128
			id := hex.EncodeToString(raw[:])
			r.Header.Set("Request-Id", id[:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:])
		}
		switch path {
		case "/i/status":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(200)
		case "/api-version":
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(405)
				return
			}
			supported := []int{}
			development := []int{}
			for v := 0; v <= 19; v++ {
				if disabledVersion(c, v) {
					continue
				}
				if v == 19 {
					development = append(development, v)
				} else {
					supported = append(supported, v)
				}
			}
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			if r.Method == http.MethodHead {
				w.WriteHeader(200)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"supported": supported, "development": development, "federation": c.Federator != nil, "domain": c.Settings.Domain})
		default:
			if business != nil && business.handle(w, r, path) {
				return
			}
			w.WriteHeader(404)
		}
	})
}

// Run currently supplies the first runnable startup slice on initialized
// native Cassandra schemas, applying native-compatible PostgreSQL migrations.
// Dependency/background initialization and business handlers remain required
// before this replacement can qualify.
func Run(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Settings.MaxConversationSize == nil {
		return errors.New("missing prekey conversation size configuration")
	}
	registrationPolicy, err := newRegistrationPolicy(c)
	if err != nil {
		return err
	}
	mailDelivery, err := newRegistrationMailDelivery(ctx, c, func(string) { log.Print("mail template variable unavailable") })
	if err != nil {
		return err
	}
	defer mailDelivery.close()
	private, public, err := c.signingKeys()
	if err != nil {
		return err
	}
	if c.Settings.PasswordPolicy == nil || c.Settings.PasswordRate == nil {
		return errors.New("missing password hashing configuration")
	}
	if c.Settings.ActivationTimeout == nil {
		return errors.New("missing registration activation configuration")
	}
	locale := c.Settings.DefaultLocale
	if locale == "" {
		locale = "en"
	}
	registrationDefaultLocale, err := parseRegistrationLocale(locale)
	if err != nil {
		return err
	}
	galleyVersion, err := registrationGalleyVersion(c)
	if err != nil {
		return err
	}
	newConferenceStatus, err := conferenceCallingNew(c)
	if err != nil {
		return err
	}
	rate, err := newPasswordRateLimiter(*c.Settings.PasswordRate)
	if err != nil {
		return err
	}
	passwords, err := newPasswordService(*c.Settings.PasswordPolicy, rate, 1)
	if err != nil {
		return err
	}
	if c.Elasticsearch == nil || c.Settings.CookieRenewAge == nil || c.Settings.CookieLimit == nil || c.Settings.CookieThrottle == nil {
		return errors.New("missing session/search index configuration")
	}
	timeout, err := inactiveTimeout(c)
	if err != nil {
		return err
	}
	indexWriter, err := newSearchIndex(*c.Elasticsearch)
	if err != nil {
		return err
	}
	defer indexWriter.close()
	cassandra, err := openCassandra(ctx, c)
	if err != nil {
		return err
	}
	defer cassandra.Close()
	postgres, err := openPostgres(ctx, c)
	if err != nil {
		return err
	}
	defer postgres.Close()
	backendNotifications, err := newBrigNotificationQueue(ctx, c)
	if err != nil {
		return err
	}
	defer backendNotifications.close()
	if err = runPostgresMigrations(ctx, postgres, time.Duration(c.PostgresPool.Acquisition)); err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	peerHTTP := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	journal, err := newUserJournal(ctx, c.AWS, peerHTTP)
	if err != nil {
		return err
	}
	internalQueue, err := newInternalEventQueue(ctx, c.InternalEvents, c.AWS, peerHTTP, c)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", c.Brig.Address())
	if err != nil {
		return errors.New("Brig listener initialization failed")
	}
	business := brigReadHandlers{config: c, users: userStore{userAuthStore{cassandra: cassandra, postgres: postgres, mode: c.Migration["user"], acquisition: time.Duration(c.PostgresPool.Acquisition)}}, sessions: sessionStore{cassandra}, peers: brigPeers{client: peerHTTP, galley: c.Galley, gundeck: c.Gundeck}}
	federationRead, err := federationReadFromConfig(c)
	if err != nil {
		return err
	}
	federationRead.useCassandra(cassandra)
	business.federationConfig = &federationRead
	providerStore := providerAccountStore{cassandra: cassandra}
	business.providerAccounts = &providerAccountHTTP{lookup: providerStore.lookup, update: providerStore.update, secondFactorEnabled: business.peers.providerSecondFactorEnabled}
	business.mlsClients = &mlsClientReads{domain: c.Settings.Domain, key: (clientStore{cassandra}).mlsPublicKey, insert: (clientStore{cassandra}).insertMLSPackages, removeSuite: (clientStore{cassandra}).deleteMLSSuite, ids: (clientStore{cassandra}).ids, get: (clientStore{cassandra}).get, packages: (clientStore{cassandra}).mlsPackages, remove: (clientStore{cassandra}).deleteMLSPackages, now: time.Now, maximum: c.Settings.KeyPackageMaximumLifetime, enabled: c.Settings.EnableMLS}
	connectionStorage := connectionStore{cassandra: cassandra, domain: c.Settings.Domain}
	business.localConnections = &connectionLocalService{domain: c.Settings.Domain, limit: c.Settings.UserMaxConnections, activated: business.users.isActivated, user: business.users.get, legalhold: business.peers.connectionLegalhold, team: business.peers.connectionTeam, get: connectionStorage.get, count: connectionStorage.count, insert: connectionStorage.insert, update: connectionStorage.update, createConversation: func(ctx context.Context, self, target gocql.UUID, connection, requestID string) (gocql.UUID, error) {
		return business.peers.createConnectionConversation(ctx, self, target, connection, requestID, c.Settings.Domain)
	}, acceptConversation: business.peers.acceptConnectionConversation, now: time.Now}
	business.connections = &connectionReads{domain: c.Settings.Domain, get: connectionStorage.get, page: connectionStorage.page}
	business.clients = &clientReadHandlers{store: clientStore{cassandra}}
	business.clientActivity = &clientActivityService{update: (clientStore{cassandra}).updateLastActive, now: time.Now}
	business.clientUpdate = &clientUpdateService{store: clientStore{cassandra}, consumable: c.Settings.ConsumableNotifications, setup: business.peers.setupConsumableClient}
	business.bulkClients = &clientBulkReads{clients: (clientStore{cassandra}).list, ids: (clientStore{cassandra}).ids}
	issuer := tokenIssuer{key: private, index: int64(c.ZAuth.Settings.KeyIndex), userTTL: c.ZAuth.Settings.UserTTL, sessionTTL: c.ZAuth.Settings.SessionTTL, accessTTL: c.ZAuth.Settings.AccessTTL, legalUserTTL: c.ZAuth.Settings.LegalHoldUserTTL, legalAccessTTL: c.ZAuth.Settings.LegalHoldAccessTTL}
	business.access = &accessTokenService{lookup: business.sessions, sessions: sessionManager{store: business.sessions, issuer: issuer, renewAge: *c.Settings.CookieRenewAge}, keys: public, now: time.Now}
	business.index = indexWriter
	business.appIDs = business.users.teamAppIDs
	business.accounts = &accountLookup{users: business.users.getMany, resolve: business.users.resolveLogin, invitations: (invitationStore{cassandra}).byEmail, enqueueDeletion: internalQueue.enqueueUserDeletion, domain: c.Settings.Domain, locale: c.Settings.DefaultLocale}
	index := searchIndexService{users: business.users.userAuthStore, team: business.peers.indexTeam, writer: indexWriter}
	business.renewal = &accessRenewalHTTP{service: accessRenewalService{
		tokens: *business.access, clients: clientStore{cassandra},
		accounts:        accountLifecycle{accounts: business.users.userAuthStore, cookies: business.sessions},
		inactiveTimeout: timeout,
	}, syncIndex: index.sync, cookieInsecure: c.Settings.CookieInsecure}
	business.auth = &reauthenticationHandler{
		accounts: business.users.get,
		passwords: authenticationService{store: business.users.userAuthStore, passwords: passwords, isSAML: func(ctx context.Context, user gocql.UUID) (bool, error) {
			record, err := business.users.get(ctx, user)
			if err != nil {
				return false, err
			}
			return storedSAML(record)
		}},
		verificationEnabled: business.peers.verificationEnabled,
		verifyCode:          verificationCodeStore{cassandra}.verify,
	}
	prekeyAWS := internalQueue.sqs
	if prekeyAWS == nil && (c.RandomPrekeys == nil || !*c.RandomPrekeys) {
		prekeyAWS, err = newBrigAWSCredentials(ctx, peerHTTP)
		if err != nil {
			return err
		}
	}
	deleteLock, err := clientLockDeletion(c, prekeyAWS)
	if err != nil {
		return err
	}
	deletionStore := clientDeletionStorage{clientStore: clientStore{cassandra}, deleteLock: deleteLock}
	business.clientDeletion = &clientDeletionService{
		lookup: deletionStore.get, reauthenticate: business.auth.passwords.reauthenticate,
		revokeLabel: func(ctx context.Context, user gocql.UUID, label string) error {
			_, err := (sessionManager{store: business.sessions}).revoke(ctx, user, nil, nil, []string{label})
			return err
		}, enqueue: internalQueue.enqueueClientDeletion, remove: deletionStore.remove,
	}
	prekeyStore, err := newPrekeyClaimStore(c, clientStore{cassandra}, prekeyAWS)
	if err != nil {
		return err
	}
	prekeyStore.exhausted = func() { log.Print("PreKeys: Optimistic lock failed") }
	federationClient, closeFederation := newPrekeyFederationClient(c)
	defer closeFederation()
	business.publicClients = &publicClientService{domain: c.Settings.Domain, remote: &federationClient, list: (clientStore{cassandra}).publicClients, warn: func(domain string) { log.Printf("Failed to fetch clients for domain %q", domain) }}
	business.mlsClients.user = business.users.get
	business.mlsClients.clients = (clientStore{cassandra}).list
	business.mlsClients.legalhold = business.peers.mlsUserLegalhold
	business.mlsClients.guard = business.peers.guardPrekeyLegalhold
	business.mlsClients.removeOne = (clientStore{cassandra}).deleteMLSOne
	business.mlsClients.claimGate = make(chan struct{}, 1)
	business.mlsClients.remote = &federationClient
	business.prekeys = &prekeyClaimService{remote: &federationClient, domain: c.Settings.Domain, maxUsers: int(*c.Settings.MaxConversationSize), claim: prekeyStore.claim, clients: (clientStore{cassandra}).list, lookup: (clientStore{cassandra}).get, remove: business.clientDeletion.execute, guard: business.peers.guardPrekeyLegalhold, federationGuard: business.peers.guardFederationPrekeyLegalhold}
	maxPermanent := 7
	if c.Settings.MaxPermanentClients != nil {
		maxPermanent = *c.Settings.MaxPermanentClients
	}
	business.clientRegistration = &clientRegistrationService{
		core:    clientRegistrationCore{store: clientStore{cassandra}, maxPermanent: maxPermanent, now: time.Now, reauthenticate: business.auth.passwords.reauthenticate},
		account: business.users.get, verificationEnabled: business.peers.verificationEnabled, verifyCode: verificationCodeStore{cassandra}.verify,
		consumable: c.Settings.ConsumableNotifications, setup: business.peers.setupConsumableClient,
		deleteOld: business.clientDeletion.execute, galley: business.peers.registerGalleyClient, event: business.peers.clientAddedEvent,
		mail: mailDelivery.newClient, domain: c.Settings.Domain, locale: c.Settings.DefaultLocale,
	}
	notifications := newSessionNotifications(ctx, business.peers)
	defer notifications.close()
	throttleTTL := int32(300)
	if value := c.Settings.Other["set2FACodeGenerationDelaySecs"]; value != nil {
		encoded, err := json.Marshal(value)
		var seconds int64
		if err != nil || json.Unmarshal(encoded, &seconds) != nil {
			return errors.New("invalid verification code throttle configuration")
		}
		throttleTTL = int32(seconds)
	}
	verificationStore := verificationCodeStore{cassandra}
	deletionCodes := accountDeletionCodes{lookup: verificationStore.deletionLookup, insert: verificationStore.deletionInsert, remove: verificationStore.deletionRemove, throttle: verificationStore.deletionThrottle, insertThrottle: verificationStore.deletionInsertThrottle, mail: mailDelivery.deletion, throttleTTL: throttleTTL}
	deletionPeers := accountDeletionPeers{peers: business.peers, cargohold: c.Cargohold}
	deletionGroups := accountDeletionGroups{member: business.peers.deletionTeamMember, page: business.users.deletionGroupPage, remove: business.users.removeDeletionGroupMember}
	deletionEvents := accountDeletionEvents{domain: c.Settings.Domain, page: connectionStorage.page, notify: notifications.submitPush, connectionEvent: business.localConnections.event, enqueue: backendNotifications.enqueue, journal: journal, now: time.Now,
		rescind: func(ctx context.Context, domain string, from, to gocql.UUID, requestID string) error {
			_, err := federationClient.call(ctx, domain, "send-connection-action", requestID, map[string]any{"from": from.String(), "from_team": nil, "to": to.String(), "action": "RemoteRescind"})
			return err
		},
	}
	// Capture the shared asynchronous notification transport for connection events
	// before taking the service method value used by account deletion.
	business.localConnections.notify = notifications.submitPush
	deletionEvents.connectionEvent = business.localConnections.event
	deletion := accountDeletionService{get: business.users.get, owner: deletionPeers.owner, password: business.users.hashedPassword,
		verify: func(ctx context.Context, user gocql.UUID, encoded, plain string) (bool, error) {
			matched, _, err := passwords.verify(ctx, passwordRateKey{kind: "user", id: user.String()}, encoded, plain)
			return matched, err
		},
		sendCode: func(ctx context.Context, user storedUser, _ string) (int64, error) {
			profile, err := user.selfProfile(c.Settings.Domain, c.Settings.DefaultLocale)
			if err != nil {
				return 0, err
			}
			return deletionCodes.send(ctx, profile, user.ID)
		},
		emailKey: business.users.deleteEmailKey, properties: (propertyStore{cassandra}).clear, tombstone: business.users.deleteAccount, invitations: business.users.deleteAccountInvitations,
		groups: deletionGroups.execute, peers: deletionPeers.remove, clients: (clientStore{cassandra}).list, removeClient: deletionStore.remove, index: index.sync, events: deletionEvents.generate, connections: connectionStorage.deleteAccount, cookies: business.sessions.deleteAll,
	}
	business.accountDeletion = &accountDeletionHTTP{service: deletion, verification: accountDeletionVerification{codes: deletionCodes, get: business.users.get, execute: deletion.execute}, keys: (propertyStore{cassandra}).keys, count: connectionStorage.deletionConnectionCount, cookies: business.sessions.list}
	business.remoteDeletion = &remoteAccountDeletion{statuses: connectionStorage.remoteDeletionAccepted, remove: connectionStorage.removeRemoteDeletedUser, notify: notifications.submitPush}
	business.localConnections.notify = notifications.submitPush
	business.publicProfiles = &publicProfileService{domain: c.Settings.Domain, locale: c.Settings.DefaultLocale, emailVisibility: c.Settings.EmailVisibility, get: business.users.get, legalhold: business.peers.connectionLegalhold, enqueueDeletion: internalQueue.enqueueUserDeletion, now: time.Now, app: func(ctx context.Context, user, team gocql.UUID, requestID string) (*publicAppInfo, error) {
		owner, err := business.users.get(ctx, user)
		if err != nil {
			return nil, err
		}
		if owner == nil {
			return nil, errPublicProfileAppOwner
		}
		_, missing, err := business.peers.galleyRPC(ctx, http.MethodGet, "/i/teams/"+team.String()+"/members/"+user.String(), requestID, nil, true, &user)
		if err != nil {
			return nil, err
		}
		if missing {
			return nil, errPublicProfileAppPermission
		}
		return business.users.publicApp(ctx, user, team)
	}}

	business.remoteConnections = &connectionRemoteService{local: *business.localConnections, mls: c.Settings.EnableMLS, call: federationClient.call, allowed: func(ctx context.Context, domain string, team *gocql.UUID) (bool, error) {
		return federationRead.connectionAllowed(ctx, domain, team, cassandra)
	}, upsert: func(ctx context.Context, self, target gocql.UUID, domain string, conv gocql.UUID, convDomain string, remote, included bool, requestID string) error {
		return business.peers.upsertConnectionConversation(ctx, self, target, domain, conv, convDomain, remote, included, requestID, c.Settings.Domain)
	}, established: business.peers.connectionMLSEstablished}

	business.connectionUpdates = &connectionUpdateService{local: *business.localConnections, mls: c.Settings.EnableMLS, accept: business.peers.acceptConnectionOptional, block: business.peers.blockConnectionConversation, unblock: func(ctx context.Context, self, conv gocql.UUID, domain string, connection *string, requestID string) (int, error) {
		return business.peers.unblockConnectionConversation(ctx, self, conv, domain, connection, requestID, galleyVersion)
	}, established: business.peers.connectionMLSEstablished}

	business.properties = newPropertyService(c, propertyStore{cassandra}, notifications.submitPush)
	codes := activationStore{cassandra}
	resets := passwordResetStore{databases: business.users.userAuthStore, mode: c.Migration["passwordReset"]}
	domains := domainRegistrationStore{databases: business.users.userAuthStore, mode: c.Migration["domainRegistration"], invalidRecord: func() { log.Print("invalid stored domain registration") }}
	activation := activationService{codes: codes, account: business.users.get, identities: business.users.userAuthStore, deletePasswordReset: resets.deleteForUser}
	business.passwordChange = &passwordChangeService{activated: business.users.isActivated, current: business.users.hashedPassword, hash: passwords.hash, verify: passwords.verify, write: business.users.setPassword, revoke: business.sessions.deleteAll}
	business.passwordReset = &passwordResetHTTP{service: passwordResetService{
		codes: resets, account: business.users.get, currentPassword: business.users.hashedPassword,
		setPassword: business.users.setPassword, deleteCookies: business.sessions.deleteAll,
		verifyPassword: passwords.verify, hashPassword: passwords.hash, mail: mailDelivery.passwordReset,
		allowDomains: c.Settings.AllowlistDomains, domain: c.Settings.Domain, locale: c.Settings.DefaultLocale,
		newCode: newPasswordResetCode, now: time.Now,
		activeEmail: func(ctx context.Context, email string) (*storedUser, error) {
			id, err := business.users.resolveLogin(ctx, loginIdentifier{email: &email})
			if err != nil || id == nil {
				return nil, err
			}
			rows, err := business.users.getMany(ctx, []gocql.UUID{*id})
			if err != nil || len(rows) != 1 {
				return nil, err
			}
			profile, err := rows[0].selfProfile(c.Settings.Domain, c.Settings.DefaultLocale)
			if err != nil {
				return nil, err
			}
			if profile.Status != "active" {
				return nil, nil
			}
			return &rows[0], nil
		},
	}}
	business.activation = &activationHTTP{lookupPair: codes.lookupPair, service: activationAPI{codes: codes, guardDomain: func(ctx context.Context, email string) error {
		return guardRegistrationDomain(ctx, email, domains.lookup)
	}, activate: activation.activateKey, clearUnvalidated: business.users.clearUnvalidatedEmail}, syncIndex: index.sync, events: activationEvents{notifications: notifications, journal: journal, domain: c.Settings.Domain, defaultLocale: c.Settings.DefaultLocale}, peers: business.peers}
	blocklist := blocklistStore{databases: business.users.userAuthStore, mode: c.Migration["blockList"]}
	business.blocklist = &blocklist
	if c.Settings.MaxTeamSize == nil {
		return errors.New("missing native maximum team size")
	}
	invites := invitationStore{cassandra}
	business.registration = &registrationHTTP{policy: registrationPolicy, mail: mailDelivery, sessions: business.access.sessions, refreshSuggested: notifications.submit, cookieInsecure: c.Settings.CookieInsecure, service: registrationService{
		invitedAccount: func(ctx context.Context, user gocql.UUID) (*selfProfile, error) {
			profiles, err := business.accounts.lookup(ctx, accountLookupRequest{IDs: []gocql.UUID{user}, IncludePending: true})
			if err != nil || len(profiles) == 0 {
				return nil, err
			}
			return &profiles[0], nil
		},
		claimInvitationKey: business.users.claimEmailKey, removePending: invites.removePending, deleteInvitation: invites.delete,
		identity:    registrationIdentity{owner: business.users.registrationEmailOwner, account: business.users.get, deleteKey: business.users.deleteEmailKey, blacklisted: blocklist.exists},
		guardDomain: business.activation.service.guardDomain, hash: passwords.hash,
		activateStored: business.users.setEmailIdentity,
		createAccount: func(ctx context.Context, account storedUser) error {
			return business.users.create(ctx, account, nil, nil)
		},
		newCode: codes.newCode, initFeature: func(ctx context.Context, user gocql.UUID) error {
			if newConferenceStatus == nil {
				return nil
			}
			return business.users.updateConferenceCalling(ctx, user, newConferenceStatus)
		},
		domain: c.Settings.Domain, locale: registrationDefaultLocale, sessionTTL: c.ZAuth.Settings.SessionTTL, activationTimeout: *c.Settings.ActivationTimeout, now: time.Now,
	}, peers: business.peers, galleyVersion: galleyVersion, activation: business.activation, index: indexWriter, invitations: registrationInvitations{lookup: invites.byCode, domain: domains.lookup, maxSize: *c.Settings.MaxTeamSize}}

	loginSessions := business.access.sessions
	loginSessions.limit = *c.Settings.CookieLimit
	loginSessions.throttle = cookieThrottle{stdDev: c.Settings.CookieThrottle.StdDev, retryAfter: c.Settings.CookieThrottle.RetryAfter}
	business.login = &loginHTTP{service: loginService{
		resolve: business.users.resolveLogin, pending: business.users.pendingLoginActivation,
		authenticate: business.auth.passwords.authenticate, account: business.users.get,
		verificationEnabled: business.peers.verificationEnabled, verifyCode: verificationCodeStore{cassandra}.verify,
		budgets: loginBudgetStore{cassandra}, limit: c.Settings.FailedLogins,
		lifecycle:       accountLifecycle{accounts: business.users.userAuthStore, cookies: business.sessions},
		inactiveTimeout: timeout, sessions: loginSessions, now: time.Now,
	}, syncIndex: index.sync, refreshSuggested: notifications.submit, cookieInsecure: c.Settings.CookieInsecure}
	server := &http.Server{Handler: newHandler(c, &business), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	feedback := startSESFeedback(ctx, mailDelivery.feedback, blocklist, c.Settings.SQSThrottleMillis)
	defer feedback.close()
	deleteThrottle := int64(100)
	if value := c.Settings.Other["setDeleteThrottleMillis"]; value != nil {
		encoded, err := json.Marshal(value)
		if err != nil || json.Unmarshal(encoded, &deleteThrottle) != nil {
			return errors.New("invalid internal deletion throttle configuration")
		}
	}
	botDeletion := serviceBotDeletion{
		removeMember: business.peers.removeServiceBotMember, account: business.users.get,
		clients: clientStore{cassandra}.list, removeClient: deletionStore.remove,
		removeAssociation: business.users.deleteServiceBot, status: business.users.updateAccountStatus,
	}
	serviceMetadata := serviceDeletionStore{cassandra}
	queuedServiceDeletion := serviceDeletion{
		lookup: serviceMetadata.lookup, pages: business.users.serviceBotPages,
		kick: botDeletion.execute, disconnect: business.peers.deleteServiceConnection,
		remove: serviceMetadata.remove,
	}
	internalListener := startInternalEvents(ctx, internalQueue, internalDeletionProcessor{get: business.users.get, deleteUser: deletion.execute, deleteClient: business.peers.deleteQueuedClient, deleteService: queuedServiceDeletion.execute, throttle: time.Duration(deleteThrottle) * time.Millisecond}, c.Settings.SQSThrottleMillis)
	defer internalListener.close()
	return serve(ctx, server, listener)
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("Brig HTTP server failed")
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return errors.New("Brig shutdown drain failed")
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return errors.New("Brig HTTP server failed")
		}
		return nil
	}
}
