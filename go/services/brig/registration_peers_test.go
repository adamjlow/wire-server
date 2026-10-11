// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestRegistrationGalleyNativeRequestsAndCurrency(t *testing.T) {
	user, team := gocql.UUID{}, gocql.UUID{1}
	currency, key := "EUR", "synthetic-icon-key"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Request-Id") != "synthetic-registration" {
			t.Error("request context missing")
		}
		switch calls {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/v19/conversations/self" || r.Header.Get("Z-User") != user.String() || r.ContentLength != 0 {
				t.Error("native self conversation request changed")
			}
		case 2:
			var body map[string]string
			if r.Method != http.MethodPut || r.URL.Path != "/i/teams/"+team.String() || r.Header.Get("Z-User") != user.String() || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 3 || body["name"] != "Synthetic team" || body["icon"] != "default" || body["icon_key"] != key {
				t.Error("native team creation request changed")
			}
		case 3:
			if r.Method != http.MethodGet || r.URL.Path != "/i/users/"+user.String()+"/team" {
				t.Error("binding lookup changed")
			}
			json.NewEncoder(w).Encode(team.String())
			return
		case 4:
			var body map[string]string
			if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 2 || body["status"] != "active" || body["currency"] != currency {
				t.Error("activation currency omitted or misplaced")
			}
		default:
			t.Error("unexpected peer request")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
	ctx := context.Background()
	if err := peer.createSelfConversation(ctx, user, 19, "synthetic-registration"); err != nil {
		t.Fatal(err)
	}
	if err := peer.createRegistrationTeam(ctx, user, team, registrationTeam{Name: "Synthetic team", Icon: "default", IconKey: &key, Currency: &currency}, "synthetic-registration"); err != nil {
		t.Fatal(err)
	}
	if err := peer.activateBindingTeamCurrency(ctx, user, "synthetic-registration", &currency); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("missing native registration requests")
	}
	c := Config{}
	if v, err := registrationGalleyVersion(c); err != nil || v != 19 {
		t.Fatal("native maximum available version changed")
	}
	c.Settings.DisabledVersions = []string{"development"}
	if v, err := registrationGalleyVersion(c); err != nil || v != 18 {
		t.Fatal("disabled Galley version selected")
	}
	for v := 0; v <= 19; v++ {
		c.Settings.DisabledVersions = append(c.Settings.DisabledVersions, "v"+strconv.Itoa(v))
	}
	if _, err := registrationGalleyVersion(c); err == nil {
		t.Fatal("missing API version accepted")
	}
}

func TestRegistrationSSOJoinNativeMemberBodyAndRejection(t *testing.T) {
	for _, status := range []int{200, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			user, team := gocql.UUID{}, gocql.UUID{1}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Request-Id") != "synthetic-sso" {
					t.Error("SSO request context missing")
				}
				if calls == 1 {
					var body map[string]map[string]json.RawMessage
					if r.Method != http.MethodPost || r.URL.Path != "/i/teams/"+team.String()+"/members" || r.Header.Get("Z-User") != user.String() || json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("SSO membership request changed")
					}
					member := body["member"]
					var permissions map[string]uint64
					var id string
					if len(body) != 1 || len(member) != 4 || json.Unmarshal(member["user"], &id) != nil || id != user.String() || json.Unmarshal(member["permissions"], &permissions) != nil || len(permissions) != 2 || permissions["self"] != 1587 || permissions["copy"] != 1587 || string(member["created_by"]) != "null" || string(member["created_at"]) != "null" {
						t.Error("default member privilege or native null metadata changed")
					}
					w.WriteHeader(status)
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/i/teams/"+team.String()+"/name" {
					t.Error("native team name path changed")
				}
				w.Write([]byte(`{"name":"Native team"}`))
			}))
			defer server.Close()
			host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			number, _ := strconv.Atoi(port)
			peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
			added, err := peer.joinRegistrationSSO(context.Background(), user, team, "synthetic-sso")
			if err != nil || added != (status == 200) || calls != 1 {
				t.Fatal("native membership response/retry changed", err)
			}
			name, err := peer.registrationTeamName(context.Background(), team, "synthetic-sso")
			if err != nil || name != "Native team" || calls != 2 {
				t.Fatal("native team name changed", err)
			}
		})
	}
}

func TestRegistrationInvitationMembershipPermissionsAndMetadata(t *testing.T) {
	for role, mask := range []uint64{8191, 5951, 1587, 1025} {
		for _, withCreator := range []bool{false, true} {
			user, team, creator := gocql.UUID{1}, gocql.UUID{2}, gocql.UUID{3}
			inv := storedInvitation{CreatedAt: time.Unix(1700000000, 123000000)}
			if withCreator {
				inv.CreatedBy = &creator
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					Member struct {
						User        string
						Permissions struct{ Self, Copy uint64 }
						CreatedBy   *string `json:"created_by"`
						CreatedAt   *string `json:"created_at"`
					}
				}
				if r.Method != "POST" || r.URL.Path != "/i/teams/"+team.String()+"/members" || r.Header.Get("Z-User") != user.String() || json.NewDecoder(r.Body).Decode(&body) != nil || body.Member.User != user.String() || body.Member.Permissions.Self != mask || body.Member.Permissions.Copy != mask {
					t.Error("native invitation member request")
				}
				if withCreator {
					if body.Member.CreatedBy == nil || *body.Member.CreatedBy != creator.String() || body.Member.CreatedAt == nil || *body.Member.CreatedAt != "2023-11-14T22:13:20.123Z" {
						t.Error("native invitation metadata lost")
					}
				} else if body.Member.CreatedBy != nil || body.Member.CreatedAt != nil {
					t.Error("unpaired invitation metadata")
				}
				w.WriteHeader(200)
			}))
			host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			number, _ := strconv.Atoi(port)
			peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
			added, err := peer.joinRegistrationMember(context.Background(), user, team, &inv, int32(role+1), "synthetic-invite")
			server.Close()
			if err != nil || !added || calls != 1 {
				t.Fatal("member request failed", err)
			}
		}
	}
}

func TestRegistrationAdmissionErrorPassesNativePolicyRejection(t *testing.T) {
	for _, scenario := range []struct {
		body   string
		status int
		want   bool
	}{
		{status: 200}, {status: 403, body: `{"code":403,"label":"too-many-members-for-legalhold","message":"synthetic native policy"}`, want: true},
		{status: 403, body: `{"code":403,"label":"synthetic"}`},
		{status: 403, body: `{"code":-1,"label":"synthetic","message":"synthetic"}`},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/i/teams/00000000-0000-0000-0000-000000000000/members/check" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Request-Id") != "synthetic-admission" || r.Header.Get("Z-User") != "" {
				t.Error("native admission request changed")
			}
			w.WriteHeader(scenario.status)
			w.Write([]byte(scenario.body))
		}))
		host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
		number, _ := strconv.Atoi(port)
		peer := brigPeers{client: server.Client(), galley: Endpoint{Host: host, Port: number}}
		err := peer.checkRegistrationJoin(context.Background(), gocql.UUID{}, "synthetic-admission")
		server.Close()
		var rejection registrationAdmissionError
		if scenario.status == 200 {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || errors.As(err, &rejection) != scenario.want {
			t.Fatal("admission rejection hidden/malformed accepted", err)
		}
	}
}
