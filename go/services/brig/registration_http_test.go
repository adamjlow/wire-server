// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalRegistrationRejectsInvalidRequestsBeforeDependencies(t *testing.T) {
	for _, c := range []struct {
		method, media, body string
		status              int
	}{
		{"GET", "application/json", `{"name":"Synthetic"}`, 405},
		{"POST", "text/plain", `{"name":"Synthetic"}`, 415},
		{"POST", "application/json", `{"name":"Synthetic","password":"1234567"}`, 400},
		{"POST", "application/json", `{"name":"Synthetic","sso_id":{"scim_external_id":"synthetic"}}`, 400},
		{"POST", "application/json", `{"name":"Synthetic"}`, 500},
	} {
		r := httptest.NewRequest(c.method, "/i/users", strings.NewReader(c.body))
		r.Header.Set("Content-Type", c.media)
		w := httptest.NewRecorder()
		(registrationHTTP{}).handleInternal(w, r)
		if w.Code != c.status {
			t.Fatal("registration rejected at wrong boundary", w.Code)
		}
	}
	for _, c := range []struct {
		err    error
		status int
		label  string
	}{
		{errRegistrationBlacklisted, 403, "blacklisted-email"},
		{errRegistrationMissingIdentity, 403, "missing-identity"},
		{errRegistrationRestricted, 403, "user-creation-restricted"},
		{errRegistrationEphemeralDisabled, 403, "ephemeral-user-creation-disabled"},
		{errRegistrationNotAllowlisted, 403, "unauthorized"},
		{errRegistrationInvalidInvitation, 400, "invalid-invitation-code"},
		{errRegistrationSCIMName, 403, "managed-by-scim"},
		{errInviteNotAllowed, 403, "condition-failed"},
		{errInviteOtherTeam, 403, "condition-failed"},
		{registrationAdmissionError{Code: 403, Label: "native-team-policy", Message: "synthetic rejection"}, 403, "native-team-policy"},
		{errActivationKeyExists, 409, "key-exists"},
		{passwordRateExceeded{micros: 1000000}, 429, "too-many-requests"},
		{errors.New("synthetic private detail"), 500, "server-error"},
	} {
		w := httptest.NewRecorder()
		registrationHTTPError(w, c.err)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.label) || strings.Contains(w.Body.String(), "private detail") {
			t.Fatal("registration error contract or confidentiality changed")
		}
	}
}

func TestRegistrationStageDiagnosticsPreserveErrorsAndRedactDetails(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	for _, cause := range []error{errSESMailInvalidDomain, registrationAdmissionError{Code: 403, Label: "native-team-policy", Message: "synthetic rejection"}, errors.New("synthetic private credential")} {
		failure := registrationFailure{stage: "activation-mail", cause: cause}
		var admission registrationAdmissionError
		preserved := errors.Is(failure, cause)
		if typed, ok := cause.(registrationAdmissionError); ok {
			preserved = errors.As(failure, &admission) && admission.Code == typed.Code && admission.Label == typed.Label
		}
		if !preserved || strings.Contains(failure.Error(), "credential") {
			t.Fatal("private stage lost cause or exposed details")
		}
		plain, wrapped := httptest.NewRecorder(), httptest.NewRecorder()
		registrationHTTPError(plain, cause)
		registrationHTTPError(wrapped, failure)
		if plain.Code != wrapped.Code || plain.Body.String() != wrapped.Body.String() {
			t.Fatal("stage diagnostics changed HTTP error contract")
		}
	}
	if !strings.Contains(output.String(), "registration failed at activation-mail") || strings.Contains(output.String(), "credential") || strings.Contains(output.String(), "synthetic rejection") {
		t.Fatal("diagnostics missing or exposed underlying error")
	}
}
