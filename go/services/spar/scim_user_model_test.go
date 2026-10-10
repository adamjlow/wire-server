// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func scimModelObject(t *testing.T, data string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	return object
}
func TestSCIMRichInfoOriginalGoldens(t *testing.T) {
	files, err := filepath.Glob("../../../libs/wire-api/test/golden/testObject_RichInfo_user_*.json")
	if err != nil || len(files) != 20 {
		t.Fatal("original golden set changed", err, len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			object := scimModelObject(t, string(data))
			rich, err := parseRichInfo(object)
			if err != nil {
				t.Fatal("original rich info rejected", err)
			}
			encoded, err := json.Marshal(rich.object())
			if err != nil {
				t.Fatal(err)
			}
			var expected, actual any
			_ = json.Unmarshal(data, &expected)
			_ = json.Unmarshal(encoded, &actual)
			if !reflect.DeepEqual(expected, actual) {
				t.Fatal("original rich-info wire contract differs")
			}
		})
	}
}
func TestSCIMRichInfoMergeAndBounds(t *testing.T) {
	object := scimModelObject(t, `{"urn:wire:scim:schemas:profile:1.0":{"richInfo":[{"type":"FIRST","value":"original"},{"type":"second","value":"retained"}]},"urn:ietf:params:scim:schemas:extension:wire:1.0:User":{"First":"override","Third":"appended"}}`)
	rich, err := parseRichInfo(object)
	want := richInfo{{"First", "override"}, {"second", "retained"}, {"Third", "appended"}}
	if err != nil || !reflect.DeepEqual(rich, want) {
		t.Fatal("map precedence, field casing or order changed", err, rich)
	}
	duplicate := scimModelObject(t, `{"urn:wire:scim:schemas:profile:1.0":{"richInfo":{"version":0,"fields":[{"type":"Straße","value":"one"},{"type":"STRASSE","value":"two"}]}}}`)
	if _, err := parseRichInfo(duplicate); err == nil {
		t.Fatal("versioned case-fold duplicate accepted")
	}
	zeroDecimal := scimModelObject(t, `{"urn:wire:scim:schemas:profile:1.0":{"richInfo":{"version":0.0,"fields":[]}}}`)
	if _, err := parseRichInfo(zeroDecimal); err != nil {
		t.Fatal("native integral decimal version rejected", err)
	}
}
func TestSCIMUserValidationContract(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		detail     string
	}{
		{"valid", `{"userName":"Synthetic.User","externalId":"synthetic@example.test","preferredLanguage":"en-US","roles":[{"value":"partner"}]}`, 0, ""},
		{"legacy-role", `{"userName":"Synthetic.User","externalId":"synthetic@example.test","preferredLanguage":"en-US","roles":["partner"]}`, 0, ""},
		{"password", `{"userName":"synthetic","externalId":"synthetic@example.test","password":"synthetic-password"}`, 400, "Setting user passwords is not supported"},
		{"external-required", `{"userName":"synthetic"}`, 400, "externalId is required"},
		{"no-idp-no-email", `{"userName":"synthetic","externalId":"opaque"}`, 400, "Could not process externalId"},
		{"invalid-handle", `{"userName":"synthetic@invalid","externalId":"synthetic@example.test"}`, 400, "is not a valid Wire handle"},
		{"two-primaries", `{"userName":"synthetic","externalId":"synthetic@example.test","emails":[{"value":"one@example.test","primary":true},{"value":"two@example.test","primary":"TRUE"}]}`, 400, "More than one email is marked as primary"},
		{"invalid-legacy-role", `{"userName":"synthetic","externalId":"synthetic@example.test","roles":["administrator"]}`, 400, "Valid roles are owner, admin, member, partner."},
		{"invalid-role", `{"userName":"synthetic","externalId":"synthetic@example.test","roles":[{"value":"administrator"}]}`, 400, "Valid roles are owner, admin, member, partner."},
		{"uppercase-language", `{"userName":"synthetic","externalId":"synthetic@example.test","preferredLanguage":"EN"}`, 400, "Could not parse language"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, err := parseSCIMUser(scimModelObject(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			valid, err := validateSCIMModel(user, nil, 1000, "post")
			if tc.status == 0 {
				if err != nil || valid.Handle != "synthetic.user" || valid.Language == nil || *valid.Language != "en" || valid.Role == nil || *valid.Role != "partner" || valid.Email == nil || !valid.Active {
					t.Fatal("valid native model differs", valid, err)
				}
			} else {
				var e *scimError
				if !errors.As(err, &e) || e.Status != tc.status || !strings.Contains(e.Detail, tc.detail) {
					t.Fatal("validation error differs", err)
				}
			}
		})
	}
	input := scimUserInput{UserName: "synthetic", External: ptrSCIMString("opaque"), RichInfo: richInfo{{"Field", "✈✈"}}}
	idp := idpConfig{Metadata: idpMetadata{Issuer: "https://synthetic.example/idp"}}
	valid, err := validateSCIMModel(input, &idp, 7, "post")
	if err != nil || valid.Ref == nil || valid.Ref.Subject.Format != nameID11+"unspecified" {
		t.Fatal("IdP opaque external ID", err)
	}
	_, err = validateSCIMModel(input, &idp, 6, "post")
	var e *scimError
	if !errors.As(err, &e) || e.Status != 413 {
		t.Fatal("rich info limit not enforced", err)
	}
}
func ptrSCIMString(value string) *string { return &value }
func TestSCIMLegacyStateAndNullableMetadata(t *testing.T) {
	uid := scimTestID(t, "000000000001")
	created := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	legacy, err := decodeSCIMCreation(&uid, nil)
	if err != nil || legacy.Creating || legacy.UID != uid {
		t.Fatal("legacy status is not created")
	}
	one := int32(1)
	creating, err := decodeSCIMCreation(&uid, &one)
	if err != nil || !creating.Creating {
		t.Fatal("incomplete creation collapsed into success")
	}
	two := int32(2)
	if _, err = decodeSCIMCreation(&uid, &two); err == nil {
		t.Fatal("unknown state accepted")
	}
	if _, err = decodeSCIMCreation(nil, nil); err == nil {
		t.Fatal("partial external row accepted")
	}
	meta, err := decodeSCIMMeta(&created, &created, nil, nil)
	if err != nil || meta.EmailType != nil || meta.EmailPrimary != nil {
		t.Fatal("legacy email metadata synthesized")
	}
	primary := false
	typ := "work"
	meta, err = decodeSCIMMeta(&created, &created, &typ, &primary)
	if err != nil || meta.EmailPrimary == nil || *meta.EmailPrimary || meta.EmailType == nil {
		t.Fatal("explicit false metadata lost")
	}
	if _, err = decodeSCIMMeta(&created, nil, nil, nil); err == nil {
		t.Fatal("partial timestamp row accepted")
	}
}
