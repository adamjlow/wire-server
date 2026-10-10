// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"errors"
	"reflect"
	"testing"
)

func scimPatchTestOperations(t *testing.T, text string) []scimPatchOperation {
	t.Helper()
	object := scimModelObject(t, text)
	ops, err := parseSCIMPatch(object)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}
func TestSCIMPatchNullAndAbsentRemainDistinct(t *testing.T) {
	typ, primary := "work", true
	user := scimUserInput{UserName: "synthetic", Emails: []scimEmail{{"synthetic@example.test", &typ, &primary}}}
	ops := scimPatchTestOperations(t, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","path":"emails[type eq \"work\"].type","value":null}]}`)
	patched, err := applySCIMPatch(user, ops)
	if err != nil || len(patched.Emails) != 1 || patched.Emails[0].Type != nil || patched.Emails[0].Primary == nil || !*patched.Emails[0].Primary || patched.Emails[0].Value != "synthetic@example.test" {
		t.Fatal("null erased identity or primary", patched, err)
	}
	ops = scimPatchTestOperations(t, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Replace","path":"emails[primary eq true].type"}]}`)
	_, err = applySCIMPatch(user, ops)
	var failure *scimError
	if !errors.As(err, &failure) || failure.Type != "invalidValue" || failure.Detail != "No value was provided" {
		t.Fatal("missing value became null", err)
	}
	ops = scimPatchTestOperations(t, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"Remove","path":"emails[primary eq true].value"}]}`)
	_, err = applySCIMPatch(user, ops)
	if !errors.As(err, &failure) || failure.Type != "invalidPath" {
		t.Fatal("email identity was removed", err)
	}
}
func TestSCIMPatchNativeCreateOnAbsentAndPrimarySelection(t *testing.T) {
	primary := true
	user := scimUserInput{UserName: "synthetic", Emails: []scimEmail{{Value: "original@example.test", Primary: &primary}}}
	ops := scimPatchTestOperations(t, `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"operations":[{"op":"ADD","path":"Emails[type EQ \"work\"].value","value":"new@example.test"}]}`)
	patched, err := applySCIMPatch(user, ops)
	if err != nil || len(patched.Emails) != 2 || patched.Emails[1].Type == nil || *patched.Emails[1].Type != "work" {
		t.Fatal("native append-on-absent changed", patched, err)
	}
	selected, err := selectSCIMEmail(patched.Emails)
	if err != nil || selected.Value != "original@example.test" {
		t.Fatal("new append displaced primary", selected, err)
	}
}
func TestSCIMPatchSchemaAndMutationBoundaries(t *testing.T) {
	user := scimUserInput{UserName: "synthetic", RichInfo: richInfo{{"Zebra", "old"}, {"Alpha", "first"}}}
	path := richMapURN + ":Zebra"
	op := scimPatchOperation{Op: "replace", Path: &scimPatchPath{Attr: scimAttr{Schema: richMapURN, Name: "Zebra"}}, Value: "new", HasValue: true}
	patched, err := applySCIMPatch(user, []scimPatchOperation{op})
	if err != nil || !reflect.DeepEqual(patched.RichInfo, richInfo{{"Alpha", "first"}, {"Zebra", "new"}}) {
		t.Fatal("native map patch order changed", patched, err)
	}
	if _, err := parseSCIMPatchPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSCIMPatchPath("URN" + path[3:]); err == nil {
		t.Fatal("schema URI was case folded")
	}
	for _, path := range []string{"password", "userName"} {
		parsed, err := parseSCIMPatchPath(path)
		if err != nil {
			t.Fatal(err)
		}
		op := scimPatchOperation{Op: "replace", Path: &parsed, Value: "synthetic", HasValue: true}
		if path == "userName" {
			op.Op = "remove"
		}
		_, err = applySCIMPatch(user, []scimPatchOperation{op})
		if err == nil {
			t.Fatal("forbidden password/mutability operation accepted", path)
		}
	}
}
func TestSCIMFilterNativeGrammarAndComparisons(t *testing.T) {
	for _, raw := range []string{`userName eq "synthetic"`, scimUserSchema + `:UserName.Value EQ "synthetic"`, `primary Ne FALSE`, `value ge "a]b"`} {
		if _, err := scimParseComparison(raw, []string{scimUserSchema}); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`userName pr`, `userName eq "synthetic" or userName eq "another"`, `urn:unknown:User:userName eq "synthetic"`, `userName eq {}`, `userName eq "synthetic" trailing`} {
		if _, err := scimParseComparison(raw, []string{scimUserSchema}); err == nil {
			t.Fatal("unsupported grammar accepted", raw)
		}
	}
	parsed, err := parseSCIMPatchPath(`emails[value eq "a]b@example.test"].type`)
	if err != nil || parsed.Filter == nil || parsed.Filter.Value != "a]b@example.test" || parsed.Sub != "type" {
		t.Fatal("quoted bracket corrupted path", parsed, err)
	}
	if scimCompareText("eq", "Work", "work") {
		t.Fatal("native case-sensitive value changed")
	}
}
