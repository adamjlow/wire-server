// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"unicode/utf8"

	"github.com/apache/cassandra-gocql-driver/v2"
)

const providerActivationScope int32 = 2

var errProviderEmailExists = errors.New("provider email already in use")

type providerRegistrationRequest struct {
	name, email, url, description string
	password                      *string
}

func (providerRegistrationRequest) String() string { return "<ProviderRegistrationRequest>" }

type providerRegistrationReply struct {
	ID       gocql.UUID `json:"id"`
	Password *string    `json:"password,omitempty"`
}

func (providerRegistrationReply) String() string { return "<ProviderRegistrationReply>" }

func decodeProviderRegistration(raw json.RawMessage) (providerRegistrationRequest, error) {
	var result providerRegistrationRequest
	fields, err := registrationObject(raw)
	if err != nil {
		return result, err
	}
	for _, field := range []struct {
		name string
		into *string
	}{{"name", &result.name}, {"email", &result.email}, {"url", &result.url}, {"description", &result.description}} {
		value, err := registrationText(fields, field.name, true)
		if err != nil {
			return result, err
		}
		*field.into = *value
	}
	if n := utf8.RuneCountInString(result.name); n < 1 || n > 128 {
		return result, errors.New("invalid provider name")
	}
	if n := utf8.RuneCountInString(result.description); n < 1 || n > 1024 {
		return result, errors.New("invalid provider description")
	}
	result.email, err = canonicalRegistrationEmail(result.email)
	if err != nil {
		return result, err
	}
	url, err := storedHTTPSURL([]byte(result.url))
	if err != nil {
		return result, err
	}
	result.url = string(url)
	result.password, err = registrationText(fields, "password", false)
	if err != nil || result.password != nil && validatePlainPassword(*result.password, 6) != nil {
		return result, errors.New("invalid provider password")
	}
	return result, nil
}

func newProviderPassword() (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", errors.New("provider password generation failed")
	}
	return base64.StdEncoding.EncodeToString(random[:]), nil
}

type providerRegistrationService struct {
	auth        providerAuthHTTP
	codes       providerResetService
	create      func(context.Context, providerRegistrationRequest, string) (gocql.UUID, error)
	data        func(context.Context, gocql.UUID) (*providerAccountData, error)
	activateKey func(context.Context, gocql.UUID, *string, string) error
	mail        func(context.Context, string, string, string, string, bool) error
	approval    func(context.Context, string, string) error
	newPassword func() (string, error)
}

// Native CreateCodeOverwritePrevious does not inspect an existing code. It
// generates first, then checks/writes the throttle, then writes the full code.
func (s providerRegistrationService) overwrite(ctx context.Context, id gocql.UUID, email string) (*providerVerificationCode, error) {
	if s.codes.newValue == nil || s.codes.throttle == nil || s.codes.insertThrottle == nil || s.codes.insert == nil {
		return nil, errors.New("provider activation generation unavailable")
	}
	key, err := emailVerificationKey(email)
	if err != nil {
		return nil, err
	}
	value, err := s.codes.newValue()
	if err != nil {
		return nil, err
	}
	delay, err := s.codes.throttle(ctx, key, providerActivationScope)
	if err != nil {
		return nil, err
	}
	if delay != nil {
		return nil, deletionCodeThrottled{*delay}
	}
	if err := s.codes.insertThrottle(ctx, key, providerActivationScope, s.codes.throttleTTL); err != nil {
		return nil, err
	}
	code := providerVerificationCode{key: key, value: value, email: email, retries: 3, ttl: 86400, account: &id}
	if err := s.codes.insert(ctx, providerActivationScope, code); err != nil {
		return nil, err
	}
	return &code, nil
}

func (s providerRegistrationService) available(ctx context.Context, email string) error {
	if s.auth.byEmail == nil {
		return errors.New("provider key lookup unavailable")
	}
	id, err := s.auth.byEmail(ctx, email)
	if err != nil {
		return err
	}
	if id != nil {
		return errProviderEmailExists
	}
	return nil
}

func (s providerRegistrationService) register(ctx context.Context, ip netip.Addr, request providerRegistrationRequest) (providerRegistrationReply, error) {
	var reply providerRegistrationReply
	if err := s.available(ctx, request.email); err != nil {
		return reply, err
	}
	if s.auth.hash == nil || s.create == nil || s.mail == nil {
		return reply, errors.New("provider registration unavailable")
	}
	password, minimum := request.password, 6
	if password == nil {
		if s.newPassword == nil {
			return reply, errors.New("provider password generation unavailable")
		}
		value, err := s.newPassword()
		if err != nil {
			return reply, err
		}
		password, reply.Password, minimum = &value, &value, 8
	}
	encoded, err := s.auth.hash(ctx, passwordRateKey{kind: "ip", ip: ip}, *password, minimum)
	if err != nil {
		return reply, err
	}
	reply.ID, err = s.create(ctx, request, encoded)
	if err != nil {
		return reply, err
	}
	code, err := s.overwrite(ctx, reply.ID, request.email)
	if err != nil {
		return reply, err
	}
	err = s.mail(ctx, request.name, request.email, code.key, code.value, false)
	return reply, err
}

func (s providerRegistrationService) emailUpdate(ctx context.Context, id gocql.UUID, email string) error {
	if err := s.available(ctx, email); err != nil {
		return err
	}
	if s.mail == nil {
		return errors.New("provider activation delivery unavailable")
	}
	code, err := s.overwrite(ctx, id, email)
	if err != nil {
		return err
	}
	return s.mail(ctx, "name", email, code.key, code.value, true)
}

func (s providerRegistrationService) activate(ctx context.Context, key, value string) (*string, error) {
	if s.codes.lookup == nil || s.codes.insert == nil || s.data == nil {
		return nil, errors.New("provider activation unavailable")
	}
	code, err := s.codes.lookup(ctx, key, providerActivationScope)
	if err != nil {
		return nil, err
	}
	if code == nil || code.retries == 0 {
		return nil, errProviderVerificationCode
	}
	if subtle.ConstantTimeCompare([]byte(code.value), []byte(value)) != 1 {
		updated := *code
		updated.retries--
		if err := s.codes.insert(ctx, providerActivationScope, updated); err != nil {
			return nil, err
		}
		return nil, errProviderVerificationCode
	}
	if code.account == nil {
		return nil, errProviderVerificationCode
	}
	data, err := s.data(ctx, *code.account)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errProviderVerificationCode
	}
	if data.email != nil && *data.email == code.email {
		return nil, nil
	}
	if s.codes.remove == nil || s.auth.byEmail == nil || s.activateKey == nil || s.approval == nil {
		return nil, errors.New("provider activation mutation unavailable")
	}
	if data.email != nil {
		oldKey, err := emailVerificationKey(*data.email)
		if err != nil {
			return nil, err
		}
		if err := s.codes.remove(ctx, oldKey, providerResetScope); err != nil {
			return nil, err
		}
	}
	owner, err := s.auth.byEmail(ctx, code.email)
	if err != nil {
		return nil, err
	}
	if owner != nil && *owner != *code.account {
		return nil, errProviderEmailExists
	}
	if err := s.activateKey(ctx, *code.account, data.email, code.email); err != nil {
		return nil, err
	}
	if data.email == nil {
		if err := s.approval(ctx, data.name, code.email); err != nil {
			return nil, err
		}
	}
	return &code.email, nil
}

func providerRegistrationHTTPError(w http.ResponseWriter, err error) {
	if errors.Is(err, errProviderEmailExists) {
		authenticationWireError(w, 409, "email-exists", "The given e-mail address is in use.")
	} else {
		providerResetHTTPError(w, err)
	}
}

func (s providerRegistrationService) handle(w http.ResponseWriter, r *http.Request, path string) bool {
	register, activate, internal, update := path == "/provider/register", path == "/provider/activate", path == "/i/provider/activation-code", path == "/provider/email"
	if !register && !activate && !internal && !update {
		return false
	}
	if (activate || internal) && r.Method != http.MethodGet && r.Method != http.MethodHead || register && r.Method != http.MethodPost || update && r.Method != http.MethodPut {
		w.WriteHeader(405)
		return true
	}
	var id gocql.UUID
	var ip netip.Addr
	var key, value, email string
	var request providerRegistrationRequest
	if register {
		values := r.Header.Values("X-Forwarded-For")
		if len(values) == 0 {
			routeParseError(w, "Header X-Forwarded-For is required")
			return true
		}
		var err error
		ip, err = netip.ParseAddr(values[0])
		if err != nil || ip.Zone() != "" {
			routeParseError(w, "Error parsing header X-Forwarded-For failed: Failed reading: Failed parsing bytestring as IpAddr.")
			return true
		}
	}
	if update {
		var valid bool
		if id, valid = providerHeader(w, r); !valid {
			return true
		}
	}
	if register || update {
		if !publicClientContentType(w, r) {
			return true
		}
		raw, err := publicClientJSON(r)
		if err == nil {
			if register {
				request, err = decodeProviderRegistration(raw)
			} else {
				var decoded providerResetRequest
				decoded, err = decodeProviderReset(raw, false)
				email = decoded.email
			}
		}
		if err != nil {
			authenticationWireError(w, 400, "bad-request", "Invalid provider request")
			return true
		}
	} else if activate {
		key, value = r.URL.Query().Get("key"), r.URL.Query().Get("code")
		if len(key) != 20 || !validActivationValue(key) || !validVerificationValue(value) {
			routeParseError(w, "Invalid provider activation request")
			return true
		}
	} else {
		var err error
		email, err = canonicalRegistrationEmail(r.URL.Query().Get("email"))
		if err != nil {
			routeParseError(w, "Invalid provider email")
			return true
		}
		key, _ = emailVerificationKey(email)
	}
	if s.auth.secondFactorEnabled == nil {
		providerRegistrationHTTPError(w, errors.New("provider feature lookup unavailable"))
		return true
	}
	enabled, err := s.auth.secondFactorEnabled(r.Context(), r.Header.Get("Request-Id"))
	if err != nil {
		providerRegistrationHTTPError(w, err)
		return true
	}
	if enabled {
		authenticationWireError(w, 403, "access-denied", "Access denied.")
		return true
	}
	switch {
	case register:
		reply, err := s.register(r.Context(), ip, request)
		if err != nil {
			providerRegistrationHTTPError(w, err)
		} else {
			w.Header().Set("Content-Type", "application/json;charset=utf-8")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(reply)
		}
	case update:
		if err := s.emailUpdate(r.Context(), id, email); err != nil {
			providerRegistrationHTTPError(w, err)
		} else {
			w.WriteHeader(202)
		}
	case activate:
		email, err := s.activate(r.Context(), key, value)
		if err != nil {
			providerRegistrationHTTPError(w, err)
		} else if email == nil {
			w.WriteHeader(204)
		} else {
			readJSON(w, r, map[string]string{"email": *email})
		}
	case internal:
		if s.codes.lookup == nil {
			providerRegistrationHTTPError(w, errors.New("provider activation lookup unavailable"))
			return true
		}
		code, err := s.codes.lookup(r.Context(), key, providerActivationScope)
		if err != nil {
			providerRegistrationHTTPError(w, err)
		} else if code == nil {
			authenticationWireError(w, 404, "not-found", "Activation key not found.")
		} else {
			readJSON(w, r, map[string]string{"key": key, "code": code.value})
		}
	}
	return true
}
