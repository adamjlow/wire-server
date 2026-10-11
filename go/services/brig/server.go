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
		r.Header.Del("X-Wire-API-Version")
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
	private, public, err := c.signingKeys()
	if err != nil {
		return err
	}
	if c.Settings.PasswordPolicy == nil || c.Settings.PasswordRate == nil {
		return errors.New("missing password hashing configuration")
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
	listener, err := net.Listen("tcp", c.Brig.Address())
	if err != nil {
		return errors.New("Brig listener initialization failed")
	}
	business := brigReadHandlers{config: c, users: userStore{userAuthStore{cassandra: cassandra, postgres: postgres, mode: c.Migration["user"], acquisition: time.Duration(c.PostgresPool.Acquisition)}}, sessions: sessionStore{cassandra}, peers: brigPeers{client: peerHTTP, galley: c.Galley, gundeck: c.Gundeck}}
	business.clients = &clientReadHandlers{store: clientStore{cassandra}}
	issuer := tokenIssuer{key: private, index: int64(c.ZAuth.Settings.KeyIndex), userTTL: c.ZAuth.Settings.UserTTL, sessionTTL: c.ZAuth.Settings.SessionTTL, accessTTL: c.ZAuth.Settings.AccessTTL, legalUserTTL: c.ZAuth.Settings.LegalHoldUserTTL, legalAccessTTL: c.ZAuth.Settings.LegalHoldAccessTTL}
	business.access = &accessTokenService{lookup: business.sessions, sessions: sessionManager{store: business.sessions, issuer: issuer, renewAge: *c.Settings.CookieRenewAge}, keys: public, now: time.Now}
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
	notifications := newSessionNotifications(ctx, business.peers)
	defer notifications.close()
	codes := activationStore{cassandra}
	resets := passwordResetStore{databases: business.users.userAuthStore, mode: c.Migration["passwordReset"]}
	domains := domainRegistrationStore{databases: business.users.userAuthStore, mode: c.Migration["domainRegistration"], invalidRecord: func() { log.Print("invalid stored domain registration") }}
	activation := activationService{codes: codes, account: business.users.get, identities: business.users.userAuthStore, deletePasswordReset: resets.deleteForUser}
	business.activation = &activationHTTP{service: activationAPI{codes: codes, guardDomain: func(ctx context.Context, email string) error {
		return guardRegistrationDomain(ctx, email, domains.lookup)
	}, activate: activation.activateKey, clearUnvalidated: business.users.clearUnvalidatedEmail}, syncIndex: index.sync, events: activationEvents{notifications: notifications, journal: journal, domain: c.Settings.Domain, defaultLocale: c.Settings.DefaultLocale}, peers: business.peers}

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
