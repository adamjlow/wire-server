// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func storedSAML(user *storedUser) (bool, error) {
	if user == nil || !user.Activated || len(user.SSO) == 0 {
		return false, nil
	}
	var sso struct {
		Tenant, Subject *string
		External        *string `json:"scim_external_id"`
	}
	if json.Unmarshal(user.SSO, &sso) != nil {
		return false, errors.New("invalid stored SSO identity")
	}
	if sso.Tenant == nil && sso.Subject == nil && sso.External != nil {
		return false, nil
	}
	if sso.Tenant == nil || sso.Subject == nil || sso.External != nil {
		return false, errors.New("invalid stored SSO identity")
	}
	// Native accepts XML or a lax absolute issuer URI, and XML/email/plain
	// subject forms. Full SAML canonicalization remains a separate store task.
	xmlElement := func(value, name string) bool {
		var elem struct {
			XMLName xml.Name
			Text    string `xml:",chardata"`
		}
		return xml.Unmarshal([]byte(value), &elem) == nil && elem.XMLName.Local == name && elem.XMLName.Space == "urn:oasis:names:tc:SAML:2.0:assertion"
	}
	issuer, err := url.Parse(*sso.Tenant)
	if !xmlElement(*sso.Tenant, "Issuer") && (err != nil || issuer.Scheme == "") {
		return false, errors.New("invalid stored SSO issuer")
	}
	subject := *sso.Subject
	plain := utf8.ValidString(subject) && utf8.RuneCountInString(subject) >= 1 && utf8.RuneCountInString(subject) <= 128
	email, emailErr := mail.ParseAddress(subject)
	if !xmlElement(subject, "NameID") && !plain && (emailErr != nil || email.Address != subject) {
		return false, errors.New("invalid stored SSO subject")
	}
	return true, nil
}

type reauthenticationHandler struct {
	passwords           authenticationService
	accounts            func(context.Context, gocql.UUID) (*storedUser, error)
	verificationEnabled func(context.Context, string, string) (bool, error)
	verifyCode          func(context.Context, string, string, string) (bool, error)
}

type reauthenticationBody struct {
	Password *string `json:"password"`
	Code     *string `json:"verification_code"`
	Action   *string `json:"action"`
}

func authenticationWireError(w http.ResponseWriter, code int, label, message string) {
	// Native authentication errors use Wai.Error's application/json media type,
	// while ordinary business read responses use JSON with charset=utf-8.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "label": label, "message": message})
}

func authenticationHTTPError(w http.ResponseWriter, err error) {
	emit := func(code int, label, message string) { authenticationWireError(w, code, label, message) }
	var throttled passwordRateExceeded
	if errors.As(err, &throttled) {
		w.Header().Set("Retry-After", strconv.FormatUint(throttled.micros/1000000, 10))
		emit(429, "too-many-requests", "Please try again later.")
		return
	}
	switch {
	case errors.Is(err, authInvalidCredentials):
		emit(403, "invalid-credentials", "Authentication failed")
	case errors.Is(err, authSuspended):
		emit(403, "suspended", "Account suspended")
	case errors.Is(err, authEphemeral):
		emit(403, "ephemeral", "Account ephemeral")
	case errors.Is(err, authPendingInvitation):
		emit(403, "pending-activation", "Account pending activation")
	case errors.Is(err, authMissingPassword):
		emit(403, "missing-auth", "Re-authentication via password required")
	default:
		emit(500, "server-error", "Internal Server Error")
	}
}

func validVerificationValue(value string) bool {
	if len(value) < 6 || len(value) > 20 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '=') {
			return false
		}
	}
	return true
}

func (h reauthenticationHandler) handle(w http.ResponseWriter, r *http.Request, rawUser string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	canonical, valid := tokenUUID(rawUser)
	if !valid {
		routeParseError(w, "invalid UUID")
		return
	}
	user, _ := gocql.ParseUUID(canonical)
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || !strings.EqualFold(media, "application/json") {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	var body *reauthenticationBody
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&body)
	var trailing any
	if err != nil || body == nil || decoder.Decode(&trailing) != io.EOF {
		wireError(w, 400, "bad-request", "Invalid JSON request body")
		return
	}
	if body.Password != nil && validatePlainPassword(*body.Password, 6) != nil || body.Code != nil && !validVerificationValue(*body.Code) {
		wireError(w, 400, "bad-request", "Invalid reauthentication request")
		return
	}
	if body.Action != nil {
		if _, valid := verificationScope(*body.Action); !valid {
			wireError(w, 400, "bad-request", "Invalid verification action")
			return
		}
	}
	if err := h.passwords.reauthenticate(r.Context(), user, body.Password); err != nil {
		authenticationHTTPError(w, err)
		return
	}
	if body.Action != nil {
		record, err := h.accounts(r.Context(), user)
		if err != nil {
			authenticationHTTPError(w, err)
			return
		}
		required := false // Native non-team default is disabled/locked.
		if record != nil && record.Team != nil {
			required, err = h.verificationEnabled(r.Context(), record.Team.String(), r.Header.Get("Request-Id"))
			if err != nil {
				authenticationHTTPError(w, err)
				return
			}
		}
		saml, err := storedSAML(record)
		if err != nil {
			authenticationHTTPError(w, err)
			return
		}
		if required && !saml {
			if body.Code == nil {
				authenticationWireError(w, 403, "code-authentication-required", "Verification code required.")
				return
			}
			if record == nil || !record.Activated || record.Email == nil {
				authenticationWireError(w, 403, "code-authentication-failed", "Code authentication failed (no such email).")
				return
			}
			matched, err := h.verifyCode(r.Context(), *record.Email, *body.Action, *body.Code)
			if err != nil {
				authenticationHTTPError(w, err)
				return
			}
			if !matched {
				authenticationWireError(w, 403, "code-authentication-failed", "Code authentication failed (no such code).")
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}
