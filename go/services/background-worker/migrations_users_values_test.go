// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestUserMigrationAssetUDTWireDecodingPreservesNULLAndFiltersInvalidAssets(t *testing.T) {
	integer := gocql.NewNativeType(4, gocql.TypeInt, "")
	text := gocql.NewNativeType(4, gocql.TypeVarchar, "")
	udt := gocql.UDTTypeInfo{Keyspace: "synthetic", Name: "asset", Elements: []gocql.UDTField{{Name: "typ", Type: integer}, {Name: "key", Type: text}, {Name: "size", Type: integer}}}
	collection := gocql.NewNativeType(4, gocql.TypeList, "text").(gocql.CollectionType)
	collection.Elem = udt
	key := "003-01-11111111-1111-1111-1111-111111111111"
	// Independent ordinary driver producer, consumed by the custom UDT path.
	input := []map[string]any{{"typ": nil, "key": key, "size": nil}, {"typ": int32(0), "key": key, "size": nil}, {"typ": int32(0), "key": key, "size": int32(1)}}
	data, err := gocql.Marshal(collection, input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded nullableUserMigrationAssets
	if err := decoded.UnmarshalCQL(collection, data); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Value) != 3 || decoded.Value[0].Type != nil || decoded.Value[1].Type == nil || decoded.Value[1].Size != nil || *decoded.Value[2].Size != 1 {
		t.Fatal("NULL UDT numbers replaced by native scalar zero")
	}
	assets := filterUserMigrationAssets(decoded.Value)
	if len(assets) != 2 || assets[0].Size != nil || assets[0].Key != "3-1-11111111-1111-1111-1111-111111111111" {
		t.Fatal("invalid asset migrated, or native key/nullable size lost")
	}
	if err := decoded.UnmarshalCQL(collection, nil); err != nil || decoded.Value != nil || filterUserMigrationAssets(decoded.Value) != nil {
		t.Fatal("absent assets became replacement list")
	}
	empty, err := gocql.Marshal(collection, []map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.UnmarshalCQL(collection, empty); err != nil || decoded.Value == nil || filterUserMigrationAssets(decoded.Value) == nil {
		t.Fatal("present empty asset list became absent")
	}
}

func TestUserMigrationRichInfoValidatesDuplicatesBeforeDroppingEmptyValues(t *testing.T) {
	got, err := decodeUserMigrationRichInfo([]byte(`{"Version":0.0,"FIELDS":[{"type":"Role","value":"synthetic"},{"type":"empty","value":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Version int                      `json:"version"`
		Fields  []userMigrationRichField `json:"fields"`
	}
	if json.Unmarshal(got, &value) != nil || len(value.Fields) != 1 || value.Fields[0].Type != "Role" {
		t.Fatal("native normalization changed")
	}
	for _, input := range []string{`{"version":1,"fields":[]}`, `{"version":"0","fields":[]}`, `{"version":1e-999,"fields":[]}`, `{"version":0,"fields":[{"type":"role","value":""},{"type":"ROLE","value":"synthetic"}]}`, `{"version":0,"fields":[{"type":null,"value":"synthetic"}]}`} {
		if _, err := decodeUserMigrationRichInfo([]byte(input)); err == nil {
			t.Fatal("malformed rich info accepted")
		}
	}
}

func TestUserMigrationPictureKeepsAbsentEmptyAndObjectListsDistinct(t *testing.T) {
	if got, err := decodeUserMigrationPicture(nil); err != nil || got != nil {
		t.Fatal("absent picture changed")
	}
	if got, err := decodeUserMigrationPicture([][]byte{}); err != nil || string(got) != "[]" {
		t.Fatal("native empty picture changed")
	}
	if _, err := decodeUserMigrationPicture([][]byte{[]byte(`{"synthetic":true}`)}); err != nil {
		t.Fatal("valid picture object rejected")
	}
	if _, err := decodeUserMigrationPicture([][]byte{[]byte(`null`)}); err == nil {
		t.Fatal("non-object picture accepted")
	}
}

func TestUserMigrationLocalesUsePinnedEnumsAndNativePrefixParsing(t *testing.T) {
	for _, test := range []struct {
		value   string
		country bool
		want    string
	}{{"en-legacy", false, "en"}, {"DE-extra", true, "DE"}, {"BQ", true, "BQ"}, {"iw", false, ""}, {"EN", false, ""}, {"de", true, ""}, {"XX", true, ""}} {
		got, err := decodeUserMigrationLocale(test.value, test.country)
		if test.want == "" {
			if err == nil {
				t.Fatal("invalid native ISO enum accepted")
			}
		} else if err != nil || *got != test.want {
			t.Fatalf("native locale %s changed", test.value)
		}
	}
}
