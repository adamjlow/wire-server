// SPDX-License-Identifier: AGPL-3.0-only
package spar

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Observe the storage boundary, not individual validation helper calls. Native
// Spar consumes a validly bound request even when subsequent checks deny login.
type judgeMemory struct {
	issuer   *string
	alive    bool
	writeErr error
	events   []string
	expiry   time.Time
}

func (s *judgeMemory) authRequestIssuer(context.Context, string) (*string, error) {
	s.events = append(s.events, "request")
	return s.issuer, nil
}
func (s *judgeMemory) assertionAlive(context.Context, string) (bool, error) {
	s.events = append(s.events, "replay")
	return s.alive, nil
}
func (s *judgeMemory) storeAssertion(_ context.Context, _ string, _ time.Time, expiry time.Time, _ TTL) error {
	s.events = append(s.events, "marker")
	s.expiry = expiry
	return s.writeErr
}
func (s *judgeMemory) deleteAuthRequest(context.Context, string) error {
	s.events = append(s.events, "consume")
	return nil
}
func judgeFixture() (samlAssertion, time.Time) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	request := "_request"
	end := now.Add(5 * time.Minute)
	return samlAssertion{
		ID: "_assertion", Issuer: "https://synthetic.example/idp", Instant: now.Add(-time.Minute),
		Subject:       samlNameID{Format: nameID11 + "unspecified", Value: "synthetic-user"},
		Confirmations: []*samlConfirmation{{Request: &request, Recipient: "https://synthetic.example/acs", NotOnOrAfter: end}},
		Conditions:    &samlConditions{NotOnOrAfter: &end, Audiences: [][]string{{"https://synthetic.example/sp"}}},
		Statements:    []samlAuthnStatement{{Instant: now.Add(-time.Minute)}},
	}, now
}
func runJudge(t *testing.T, s *judgeMemory, a samlAssertion, now time.Time) samlVerdict {
	t.Helper()
	v, err := judgeSAML(context.Background(), s, []samlAssertion{a}, true, "https://synthetic.example/sp", "https://synthetic.example/acs", TTL(3600), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestJudgeBoundRequestAndOrdinaryDenials(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		mutate       func(*samlAssertion, *judgeMemory, time.Time)
	}{
		{"valid", "", func(*samlAssertion, *judgeMemory, time.Time) {}},
		{"replay", "status-failure", func(_ *samlAssertion, s *judgeMemory, _ time.Time) { s.alive = true }},
		{"recipient", "bad-recipient", func(a *samlAssertion, _ *judgeMemory, _ time.Time) {
			a.Confirmations[0].Recipient = "https://other.example/acs"
		}},
		{"audience", "audience-mismatch", func(a *samlAssertion, _ *judgeMemory, _ time.Time) {
			a.Conditions.Audiences = [][]string{{"https://other.example/sp"}}
		}},
		{"missing-audience", "missing-audience", func(a *samlAssertion, _ *judgeMemory, _ time.Time) { a.Conditions.Audiences = nil }},
		{"future-issue", "assertion-issue-instant", func(a *samlAssertion, _ *judgeMemory, n time.Time) { a.Instant = n.Add(time.Minute) }},
		{"expired-confirmation", "confirmation-expired", func(a *samlAssertion, _ *judgeMemory, n time.Time) {
			a.Confirmations[0].NotOnOrAfter = n.Add(-time.Minute)
		}},
		{"future-condition", "condition-not-before", func(a *samlAssertion, _ *judgeMemory, n time.Time) {
			b := n.Add(time.Minute + time.Nanosecond)
			a.Conditions.NotBefore = &b
		}},
		{"expired-session", "authn-expired", func(a *samlAssertion, _ *judgeMemory, n time.Time) {
			end := n.Add(-time.Minute)
			a.Statements[0].SessionEnd = &end
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, n := judgeFixture()
			s := &judgeMemory{issuer: &a.Issuer}
			tc.mutate(&a, s, n)
			v := runJudge(t, s, a, n)
			if tc.reason == "" {
				if v.User == nil || len(v.Reasons) != 0 {
					t.Fatal("valid assertion denied", v)
				}
			} else {
				if v.User != nil || len(v.Reasons) != 1 || v.Reasons[0].Kind != tc.reason {
					t.Fatal("unexpected denial", v)
				}
			}
			want := []string{"request", "replay", "marker", "consume"}
			if s.alive {
				want = []string{"request", "replay", "consume"}
			}
			if !reflect.DeepEqual(s.events, want) {
				t.Fatal("request/replay effects", s.events)
			}
			if !s.alive && !s.expiry.Equal(*a.Conditions.NotOnOrAfter) {
				t.Fatal("replay expiry must not include clock tolerance")
			}
		})
	}
}
func TestJudgeFatalBindingDoesNotConsumeRequest(t *testing.T) {
	for _, reason := range []string{"no-in-response-to", "bad-in-response-to", "issuer-mismatch"} {
		t.Run(reason, func(t *testing.T) {
			a, n := judgeFixture()
			s := &judgeMemory{issuer: &a.Issuer}
			want := []string{"request"}
			switch reason {
			case "no-in-response-to":
				a.Confirmations[0].Request = nil
				want = nil
			case "bad-in-response-to":
				s.issuer = nil
			case "issuer-mismatch":
				other := "https://other.example/idp"
				s.issuer = &other
			}
			v := runJudge(t, s, a, n)
			if v.User != nil || len(v.Reasons) != 1 || v.Reasons[0].Kind != reason || !reflect.DeepEqual(s.events, want) {
				t.Fatal("fatal binding boundary", v, s.events)
			}
		})
	}
}
func TestJudgeStorageFailureCannotGrantOrConsume(t *testing.T) {
	a, n := judgeFixture()
	failure := errors.New("synthetic storage failure")
	s := &judgeMemory{issuer: &a.Issuer, writeErr: failure}
	v, err := judgeSAML(context.Background(), s, []samlAssertion{a}, true, "https://synthetic.example/sp", "https://synthetic.example/acs", TTL(3600), func() time.Time { return n })
	if !errors.Is(err, failure) || v.User != nil || !reflect.DeepEqual(s.events, []string{"request", "replay", "marker"}) {
		t.Fatal("storage failure granted or consumed", v, err, s.events)
	}
}
func TestJudgeClockToleranceAndMultipleSubjects(t *testing.T) {
	a, n := judgeFixture()
	a.Instant = n.Add(time.Minute - time.Nanosecond)
	b := n.Add(time.Minute)
	a.Conditions.NotBefore = &b
	a.Confirmations[0].NotBefore = &b
	a.Confirmations[0].NotOnOrAfter = n.Add(-time.Minute + time.Nanosecond)
	if v := runJudge(t, &judgeMemory{issuer: &a.Issuer}, a, n); v.User == nil {
		t.Fatal("native boundary tolerance", v)
	}
	other := a
	other.Subject.Value = "other-user"
	v, err := judgeSAML(context.Background(), &judgeMemory{issuer: &a.Issuer}, []samlAssertion{a, other}, true, "https://synthetic.example/sp", "https://synthetic.example/acs", TTL(3600), func() time.Time { return n })
	if err != nil || v.User != nil || len(v.Reasons) != 1 || v.Reasons[0].Kind != "bad-user-refs" {
		t.Fatal("conflicting signed identities granted", v, err)
	}
	// The native writer's denial takes precedence over the folded conflicting
	// identity verdict, while still preventing any session grant.
	v, err = judgeSAML(context.Background(), &judgeMemory{issuer: &a.Issuer}, []samlAssertion{a, other}, false, "https://synthetic.example/sp", "https://synthetic.example/acs", TTL(3600), func() time.Time { return n })
	if err != nil || v.User != nil || len(v.Reasons) != 1 || v.Reasons[0].Kind != "status-failure" {
		t.Fatal("ordinary denial precedence differs", v, err)
	}
}
