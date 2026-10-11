// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrationReusesOriginalNativeRequestGoldens(t *testing.T) {
	root := filepath.Join("..", "..", "..", "libs", "wire-api", "test", "golden")
	for _, group := range []struct {
		pattern string
		public  bool
		count   int
	}{
		{"testObject_NewUser_user_*.json", false, 8},
		{"testObject_NewUserPublic_user_*.json", true, 1},
	} {
		files, err := filepath.Glob(filepath.Join(root, group.pattern))
		if err != nil || len(files) != group.count {
			t.Fatal("original request fixtures missing")
		}
		for _, file := range files {
			t.Run(filepath.Base(file), func(t *testing.T) {
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				request, err := decodeRegistration(bytes.NewReader(raw), group.public)
				if err != nil {
					t.Fatal(err)
				}
				var expected struct {
					Name            string
					Password, Label *string
					Accent          *int32 `json:"accent_id"`
				}
				if json.Unmarshal(raw, &expected) != nil || request.User.Name != expected.Name || !sameRegistrationString(request.Password, expected.Password) || !sameRegistrationString(request.Label, expected.Label) {
					t.Fatal("native registration fields changed")
				}
				if expected.Accent != nil && (request.User.Accent == nil || *request.User.Accent != *expected.Accent) {
					t.Fatal("native accent changed")
				}
			})
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "testObject_BindingNewTeamUser_user_*.json"))
	if len(files) != 2 {
		t.Fatal("original team fixtures missing")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRegistrationTeam(raw); err != nil {
			t.Fatalf("native team fixture %s: %v", filepath.Base(file), err)
		}
	}
	files, _ = filepath.Glob(filepath.Join(root, "testObject_Locale_user_*.json"))
	if len(files) != 20 {
		t.Fatal("original locale fixtures missing")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var locale string
		if json.Unmarshal(raw, &locale) != nil {
			t.Fatal("native locale fixture malformed")
		}
		got, err := parseRegistrationLocale(locale)
		if err != nil {
			t.Fatal(err)
		}
		actual := got.Language
		if got.Country != nil {
			actual += "-" + *got.Country
		}
		if actual != locale {
			t.Fatal("native locale changed")
		}
	}
}
func sameRegistrationString(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func TestRegistrationOriginAndPublicRestrictions(t *testing.T) {
	for _, c := range []struct {
		fields        string
		public, valid bool
	}{
		{``, true, true},
		{`,"invitation_code":""`, true, true},
		{`,"team_code":"abc"`, true, false},
		{`,"team_code":"abc","password":"12345678"`, true, true},
		{`,"team":{"name":"Team","icon":"default"},"password":"12345678"`, true, true},
		{`,"sso_id":{"scim_external_id":""},"team_id":"00000000-0000-0000-0000-000000000001"`, false, true},
		{`,"sso_id":{"scim_external_id":""},"team_id":"00000000-0000-0000-0000-000000000001"`, true, false},
		{`,"sso_id":{"scim_external_id":""}`, false, false},
		{`,"team_id":"00000000-0000-0000-0000-000000000001"`, false, false},
		{`,"invitation_code":"a","team_code":"b","password":"12345678"`, false, false},
		{`,"uuid":"00000000-0000-0000-0000-000000000001"`, false, true},
		{`,"uuid":"00000000-0000-0000-0000-000000000001"`, true, false},
		{`,"managed_by":"wire"`, true, true},
		{`,"managed_by":"scim"`, true, false},
		{`,"managed_by":"scim"`, false, true},
		{`,"expires_in":604800`, true, true},
		{`,"expires_in":1,"email":"synthetic@example.com"`, true, false},
	} {
		request, err := decodeRegistration(strings.NewReader(`{"name":"Synthetic"`+c.fields+`}`), c.public)
		if (err == nil) != c.valid {
			t.Fatal("origin/public expectation changed", err)
		}
		if strings.Contains(fmt.Sprint(request), "12345678") {
			t.Fatal("request diagnostic leaks password")
		}
	}
}

func TestRegistrationRejectsMalformedFieldsBeforeOriginSelection(t *testing.T) {
	for _, fields := range []string{
		`"name":""`, `"name":null`, `"name":4`, `"name":"Synthetic","password":"1234567"`,
		`"name":"Synthetic","picture":[null]`, `"name":"Synthetic","assets":[{"type":"image","key":"3-9-00000000-0000-0000-0000-000000000001"}]`,
		`"name":"Synthetic","email":"synthetic@example.com","team_id":4`,
		`"name":"Synthetic","invitation_code":"abc","team":{"name":"Team","icon":false}`,
		`"name":"Synthetic","team":{"name":"Team","icon":"default","currency":"ZZZ"},"password":"12345678"`,
		`"name":"Synthetic","locale":"EN"`, `"name":"Synthetic","expires_in":0`, `"name":"Synthetic","expires_in":604801`,
		`"name":"Synthetic","accent_id":2147483648`, `"name":"Synthetic","accent_id":1.5`, `"name":"Synthetic","accent_id":1e999999999999999999999`,
		`"name":"Synthetic","supported_protocols":["other"]`, `"name":"Synthetic","managed_by":false`,
		`"name":"Synthetic","unknown":"\ud800"`,
	} {
		if _, err := decodeRegistration(strings.NewReader(`{`+fields+`}`), false); err == nil {
			t.Fatal("malformed native field accepted")
		}
	}
	for _, body := range []string{`null`, `[]`, `{"name":"Synthetic"} {}`, "{\"name\":\"\xff\"}"} {
		if _, err := decodeRegistration(strings.NewReader(body), false); err == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
}

func TestRegistrationPreservesNativeCanonicalizationAndDefaults(t *testing.T) {
	request, err := decodeRegistration(strings.NewReader(`{"name":"Synthetic","email":"(comment)synthetic@(comment)example.com.","locale":"en-USignored","assets":[{"type":"image","key":"03-01-00000000-0000-0000-0000-00000000000A","size":"complete"}],"accent_id":-2.147483648e9,"supported_protocols":["mls","mls","proteus"],"unknown":true}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if request.User.Email == nil || *request.User.Email != "synthetic@example.com" || request.User.Locale.Country == nil || *request.User.Locale.Country != "US" || *request.User.Accent != -2147483648 || *request.User.Protocols != 3 || request.User.Assets[0].Key != "3-1-00000000-0000-0000-0000-00000000000a" || *request.User.Assets[0].Size != 1 {
		t.Fatal("native canonical fields changed")
	}
	request, err = decodeRegistration(strings.NewReader(`{"name":"Synthetic","expires_in":1.2e1,"locale":"en-zz","assets":null,"supported_protocols":[]}`), true)
	if err != nil || *request.User.ExpiresIn != 12 || request.User.Locale.Language != "en" || request.User.Locale.Country != nil || *request.User.Protocols != 0 || len(request.User.Assets) != 0 {
		t.Fatal("native optional/canonical behavior changed", err)
	}
}

func TestRegistrationReusesNativeEmailAndSSOContracts(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "background-worker", "testdata", "native-email-examples.json"))
	if err != nil {
		t.Fatal(err)
	}
	var emailExamples struct {
		Cases []struct {
			Value string
			Valid bool
		} `json:"source_cases"`
	}
	if json.Unmarshal(raw, &emailExamples) != nil || len(emailExamples.Cases) == 0 {
		t.Fatal("native email examples missing")
	}
	for _, example := range emailExamples.Cases {
		_, err := canonicalRegistrationEmail(example.Value)
		if (err == nil) != example.Valid {
			t.Fatal("native email validity changed")
		}
	}
	for _, fixture := range []string{"testObject_UserSSOId_user_2.json", "testObject_UserSSOId_user_9.json", "testObject_UserSSOId_user_13.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "libs", "wire-api", "test", "golden", fixture))
		if err != nil {
			t.Fatal(err)
		}
		actual, err := decodeRegistrationSSO(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		var got, want map[string]string
		if json.Unmarshal(actual, &got) != nil || json.Unmarshal(raw, &want) != nil || len(got) != len(want) {
			t.Fatal("native SSO serialization changed")
		}
		for key, value := range want {
			if got[key] != value {
				t.Fatal("native SSO field changed")
			}
		}
	}
}
