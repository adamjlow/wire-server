// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestSelfProfileNativeGoldenJSON(t *testing.T) {
	// Reuse the existing upstream expected response without changing its fields
	// or assertions. This covers the wire model, not storage/API qualification.
	data, err := os.ReadFile("../../../libs/wire-api/test/golden/testObject_SelfProfile_user_1.json")
	if err != nil {
		t.Fatal(err)
	}
	var profile selfProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual any
	if json.Unmarshal(data, &expected) != nil || json.Unmarshal(encoded, &actual) != nil {
		t.Fatal("invalid JSON")
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("native SelfProfile JSON fields changed")
	}
}

func TestStoredProfileDoesNotExposeUnactivatedIdentity(t *testing.T) {
	id, _ := gocql.ParseUUID("00000000-0000-0000-0000-000000000001")
	email := "synthetic@example.test"
	status, protocols := int32(3), int32(0)
	user := storedUser{ID: id, Name: "Synthetic user", Email: &email, SSO: json.RawMessage(`{"scim_external_id":"synthetic"}`), Status: &status, Protocols: &protocols}
	view, err := user.selfProfile("example.test", "en")
	if err != nil {
		t.Fatal(err)
	}
	if view.Email != nil || len(view.SSO) > 0 || view.Status != "ephemeral" || view.Expires != nil || !reflect.DeepEqual(view.Protocols, []string{"proteus"}) || view.Picture == nil || view.Assets == nil || !view.Searchable {
		t.Fatal("legacy storage defaults or identity visibility changed")
	}
	user.Activated = true
	view, err = user.selfProfile("example.test", "en")
	if err != nil || view.Email == nil || *view.Email != email || len(view.SSO) == 0 {
		t.Fatal("activated identity missing")
	}
}
