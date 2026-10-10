// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Optional owned construction fixture after the existing crypto assertions.
// This is never linked into cmd/spar. Private synthetic signing keys stay in
// this process; service APIs, rather than this helper, create current state.
func runSwitchbackFixture(t *testing.T) {
	configPath := os.Getenv("SPAR_SWITCHBACK_FIXTURE_CONFIG")
	if configPath == "" {
		return
	}
	if os.Getuid() != 65534 || os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		t.Fatal("switchback fixture requires owned nonroot disposable environment")
	}
	config, err := LoadConfig(configPath)
	if err != nil || config.Cassandra.Keyspace != "spar_test" {
		t.Fatal("switchback fixture configuration")
	}
	session, err := openStore(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	store := cassandraStore{session: session}
	issuer := "https://synthetic.example/switchback/" + gocql.TimeUUID().String()
	_, cert, key := signedTestResponseWithKey(t, 1024, qualificationResponseSource("switchback", ""))
	metadata := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + issuer + `"><IDPSSODescriptor protocolSupportEnumeration="` + protocolNS + `"><KeyDescriptor use="signing"><ds:KeyInfo xmlns:ds="` + signatureNS + `"><ds:X509Data><ds:X509Certificate>` + base64.StdEncoding.EncodeToString(cert.Raw) + `</ds:X509Certificate></ds:X509Data></ds:KeyInfo></KeyDescriptor><SingleSignOnService Binding="` + postBinding + `" Location="https://synthetic.example/switchback-idp"/></IDPSSODescriptor></EntityDescriptor>`
	rows := func(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
		iter := store.read(ctx, query, args...).Iter()
		result := []map[string]any{}
		for {
			row := map[string]any{}
			if !iter.MapScan(row) {
				break
			}
			result = append(result, row)
		}
		return result, iter.Close()
	}
	var stateMutex sync.Mutex
	var activeTeam, activeIdP, activeUser gocql.UUID
	var external, legacyToken, activeToken string
	activeIssuer := issuer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metadata", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(metadata))
	})
	mux.HandleFunc("POST /state", func(w http.ResponseWriter, r *http.Request) {
		stateMutex.Lock()
		defer stateMutex.Unlock()
		var input struct {
			Action, Team, IdP, User, External, Token, TokenID, Issuer string
			Requests, Assertions                                      []string
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 100<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			http.Error(w, "invalid synthetic state input", 400)
			return
		}
		var output any
		var err error
		switch input.Action {
		case "bind":
			activeTeam, err = gocql.ParseUUID(input.Team)
			if err == nil {
				activeIdP, err = gocql.ParseUUID(input.IdP)
			}
			if err == nil {
				activeUser, err = gocql.ParseUUID(input.User)
			}
			external = input.External
			activeToken = input.Token
			activeIssuer = input.Issuer
			if !strings.HasPrefix(activeIssuer, issuer) {
				err = errors.New("invalid synthetic issuer binding")
			}
			output = map[string]any{"issuer": issuer, "team": activeTeam, "idp": activeIdP, "user": activeUser}
		case "legacy-token":
			var tokenID gocql.UUID
			tokenID, err = gocql.ParseUUID(input.TokenID)
			if activeTeam == (gocql.UUID{}) || !strings.HasPrefix(input.Token, "synthetic-switchback-") {
				err = errors.New("unbound synthetic legacy token")
			}
			if err == nil {
				info := tokenInfo{Team: activeTeam, ID: tokenID, CreatedAt: time.Now().UTC().Truncate(time.Millisecond), IdP: &activeIdP, Description: "synthetic legacy switchback", Name: tokenID.String()}
				legacyToken = input.Token
				batch := store.batch(r.Context())
				for _, table := range []string{"team_provisioning_by_token", "team_provisioning_by_team"} {
					batch.Query("INSERT INTO "+table+" (token_, team, id, created_at, idp, descr) VALUES (?, ?, ?, ?, ?, ?)", input.Token, info.Team, info.ID, info.CreatedAt, info.IdP, info.Description)
				}
				err = session.ExecuteBatch(batch)
			}
			output = map[string]any{"token_id": tokenID}
		case "snapshot":
			if activeTeam == (gocql.UUID{}) {
				err = errors.New("unbound state snapshot")
				break
			}
			state := map[string]any{}
			queries := []struct {
				name, query string
				args        []any
			}{
				{"idp", "SELECT * FROM idp WHERE idp = ?", []any{activeIdP}},
				{"team_idp", "SELECT * FROM team_idp WHERE team = ?", []any{activeTeam}},
				{"issuer_idp_v2", "SELECT * FROM issuer_idp_v2 WHERE issuer = ?", []any{activeIssuer}},
				{"tokens", "SELECT * FROM team_provisioning_by_team WHERE team = ?", []any{activeTeam}},
				{"token_lookup", "SELECT * FROM team_provisioning_by_token WHERE token_ = ?", []any{tokenHash(activeToken)}},
				{"legacy_token_lookup", "SELECT * FROM team_provisioning_by_token WHERE token_ IN ?", []any{[]string{legacyToken, tokenHash(legacyToken)}}},
				{"user_v2", "SELECT * FROM user_v2 WHERE issuer = ?", []any{activeIssuer}},
				{"legacy_user", "SELECT * FROM user WHERE issuer = ?", []any{activeIssuer}},
				{"scim_external", "SELECT * FROM scim_external WHERE team = ? AND external_id = ?", []any{activeTeam, external}},
				{"scim_meta", "SELECT * FROM scim_user_times WHERE uid = ?", []any{activeUser}},
			}
			for _, query := range queries {
				state[query.name], err = rows(r.Context(), query.query, query.args...)
				if err != nil {
					break
				}
			}
			for _, table := range []string{"authreq", "verdict", "authresp"} {
				if err != nil {
					break
				}
				column, ids := "req", input.Requests
				if table == "authresp" {
					column, ids = "resp", input.Assertions
				}
				entries := map[string]any{}
				for _, id := range ids {
					if len(id) > 100 || !strings.HasPrefix(id, "_") {
						err = errors.New("invalid synthetic SAML ID")
						break
					}
					query := "SELECT * FROM " + table + " WHERE " + column + " = ?"
					if table == "authreq" {
						query = "SELECT req, idp_issuer, TTL(idp_issuer) AS ttl FROM authreq WHERE req = ?"
					}
					if table == "verdict" {
						query = "SELECT req, format_con, format_mobile_success, format_mobile_error, cookie_label, TTL(format_con) AS ttl FROM verdict WHERE req = ?"
					}
					entries[id], err = rows(r.Context(), query, id)
					if err != nil {
						break
					}
				}
				state[table] = entries
			}
			output = state
		default:
			err = errors.New("unknown fixture action")
		}
		if err != nil {
			t.Error("switchback state fixture", err)
			http.Error(w, "synthetic state fixture failed", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(output)
	})
	mux.HandleFunc("POST /sign", func(w http.ResponseWriter, r *http.Request) {
		var input struct{ Request, Metadata, Subject, AssertionID string }
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 100<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			http.Error(w, "invalid synthetic signing input", 400)
			return
		}
		encoded, err := base64.StdEncoding.DecodeString(input.Request)
		if err != nil {
			http.Error(w, "invalid synthetic AuthnRequest", 400)
			return
		}
		request, err := parseXMLNode(encoded)
		if err != nil || request.Name.Space != protocolNS || request.Name.Local != "AuthnRequest" || request.attr("ID") == "" {
			http.Error(w, "invalid synthetic AuthnRequest", 400)
			return
		}
		meta, err := parseXMLNode([]byte(input.Metadata))
		if err != nil || meta.attr("entityID") == "" || len(meta.children("urn:oasis:names:tc:SAML:2.0:metadata", "SPSSODescriptor")) != 1 {
			http.Error(w, "invalid synthetic SP metadata", 400)
			return
		}
		services := meta.children("urn:oasis:names:tc:SAML:2.0:metadata", "SPSSODescriptor")[0].children("urn:oasis:names:tc:SAML:2.0:metadata", "AssertionConsumerService")
		if len(services) != 1 || services[0].attr("Binding") != postBinding {
			http.Error(w, "invalid synthetic SP consumer", 400)
			return
		}
		assertionID := input.AssertionID
		if assertionID == "" {
			assertionID = "_" + gocql.TimeUUID().String()
		}
		if len(assertionID) > 100 || !strings.HasPrefix(assertionID, "_") || !strings.HasSuffix(input.Subject, "@example.test") {
			http.Error(w, "invalid synthetic subject/assertion", 400)
			return
		}
		escape := func(value string) string { return nativeXMLEscape(value, true) }
		instant := time.Now().UTC().Add(-5 * time.Second).Format(time.RFC3339Nano)
		end := time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
		recipient := escape(services[0].attr("Location"))
		reqID := escape(request.attr("ID"))
		source := fmt.Sprintf(`<samlp:Response xmlns:samlp="%s" xmlns:saml="%s" ID="_%s" Version="2.0" IssueInstant="%s" InResponseTo="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer><samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status><saml:Assertion ID="%s" Version="2.0" IssueInstant="%s"><saml:Issuer>%s</saml:Issuer><saml:Subject><saml:NameID Format="%semailAddress">%s</saml:NameID><saml:SubjectConfirmation Method="%s"><saml:SubjectConfirmationData Recipient="%s" NotOnOrAfter="%s" InResponseTo="%s"/></saml:SubjectConfirmation></saml:Subject><saml:Conditions NotOnOrAfter="%s"><saml:AudienceRestriction><saml:Audience>%s</saml:Audience></saml:AudienceRestriction></saml:Conditions><saml:AuthnStatement AuthnInstant="%s"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement></saml:Assertion></samlp:Response>`, protocolNS, assertionNS, gocql.TimeUUID(), instant, reqID, recipient, issuer, escape(assertionID), instant, issuer, nameID11, escape(input.Subject), bearerMethod, recipient, end, reqID, end, escape(meta.attr("entityID")), instant)
		doc := etree.NewDocument()
		if err := doc.ReadFromString(source); err != nil {
			t.Error(err)
			http.Error(w, "synthetic XML construction failed", 500)
			return
		}
		signing, err := dsig.NewSigningContext(key, [][]byte{cert.Raw})
		if err != nil {
			t.Error(err)
			return
		}
		signing.Hash, signing.IdAttribute, signing.Prefix = crypto.SHA256, "ID", "ds"
		signing.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
		assertion := etreeChildren(doc.Root(), assertionNS, "Assertion")[0]
		assertion.CreateAttr("xmlns:saml", assertionNS)
		assertion.CreateAttr("xmlns:samlp", protocolNS)
		signed, err := signing.SignEnveloped(assertion)
		if err != nil {
			t.Error(err)
			return
		}
		doc.Root().RemoveChild(assertion)
		doc.Root().AddChild(signed)
		normalized := etree.NewDocument()
		_ = normalized.ReadFromBytes(documentBytes(t, doc))
		assertion = etreeChildren(normalized.Root(), assertionNS, "Assertion")[0]
		signature := etreeChildren(assertion, signatureNS, "Signature")[0]
		assertion.RemoveChild(signature)
		assertion.InsertChildAt(1, signature)
		if _, err := verifiedAssertions(documentBytes(t, normalized), nil); err == nil {
			t.Error("fixture accepted without configured key")
		}
		// Verify construction before submitting it to either actual service.
		if _, err := verifiedAssertions(documentBytes(t, normalized), []*x509.Certificate{cert}); err != nil {
			t.Error(err)
			http.Error(w, "synthetic signature construction failed", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"xml": string(documentBytes(t, normalized)), "request_id": request.attr("ID"), "assertion_id": assertionID, "issuer": issuer})
	})
	listener, err := net.Listen("tcp", "127.0.0.1:8123")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stopped)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}
