// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestActivationEventReusesNativeGolden(t *testing.T) {
	id, _ := gocql.ParseUUID("539d9183-32a5-4fc4-ba5c-4634454e7585")
	team, _ := gocql.ParseUUID("bb843450-b2f5-4ec8-90bd-52c7d5f1d22e")
	language, country, text := "tn", "SB", "text status"
	status := int32(2)
	account := storedUser{ID: id, Name: "alice", Accent: 1, Language: &language, Country: &country, Status: &status, Team: &team, TextStatus: &text}
	got, err := activationEventBody(&activatedIdentity{user: id, first: true, account: &account}, "foo.example.com", "en")
	if err != nil {
		t.Fatal(err)
	}
	native, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_UserEvent_2.json")
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if json.Unmarshal(got, &actual) != nil || json.Unmarshal(native, &expected) != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("native activation event golden mismatch")
	}
	push, err := nativeSelfPush(id, "any", got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []struct {
		Recipients []struct{ Route string }
		Payload    []json.RawMessage
	}
	if json.Unmarshal(push, &decoded) != nil || len(decoded) != 1 || len(decoded[0].Recipients) != 1 || decoded[0].Recipients[0].Route != "any" || len(decoded[0].Payload) != 1 {
		t.Fatal("activation self push changed")
	}
}
func TestEmailActivationEventContainsOnlyNativeIdentity(t *testing.T) {
	event, err := activationEventBody(&activatedIdentity{user: gocql.UUID{}, email: "synthetic@example.com"}, "example.com", "en")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	json.Unmarshal(event, &payload)
	user, ok := payload["user"].(map[string]any)
	if payload["type"] != "user.update" || !ok || len(user) != 2 || user["email"] != "synthetic@example.com" || user["id"] != (gocql.UUID{}).String() {
		t.Fatal("native email update changed")
	}
}

func TestActivationPushBestEffortAndJournalFailure(t *testing.T) {
	delivered := make(chan bool, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/i/push/v2" {
			var body []struct {
				Recipients []struct{ Route string }
				Payload    []struct{ Type string }
			}
			valid := json.NewDecoder(r.Body).Decode(&body) == nil && len(body) == 1 && len(body[0].Recipients) == 1 && body[0].Recipients[0].Route == "direct" && len(body[0].Payload) == 1 && body[0].Payload[0].Type == "user.update" && r.Header.Get("Request-Id") == "synthetic-activation"
			delivered <- valid
			w.WriteHeader(503)
		} else {
			w.WriteHeader(403)
			w.Write([]byte(`<ErrorResponse><Error><Code>AccessDenied</Code></Error></ErrorResponse>`))
		}
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	notifications := newSessionNotifications(context.Background(), brigPeers{client: server.Client(), gundeck: Endpoint{Host: host, Port: number}})
	defer notifications.close()
	journal := &userJournal{client: server.Client(), endpoint: server.URL, queue: server.URL + "/queue", region: "us-east-1", now: time.Now, credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}, nil
	})}
	events := activationEvents{notifications: notifications, journal: journal}
	if err := events.generate(context.Background(), &activatedIdentity{user: gocql.UUID{}, email: "synthetic@example.com"}, "synthetic-activation"); err == nil {
		t.Fatal("configured journal failure ignored")
	}
	select {
	case valid := <-delivered:
		if !valid {
			t.Fatal("native activation push changed")
		}
	case <-time.After(time.Second):
		t.Fatal("journal failure prevented scheduled push")
	}
	notifications.close()
	select {
	case <-delivered:
		t.Fatal("nonreplayable activation push retried")
	default:
	}
}
