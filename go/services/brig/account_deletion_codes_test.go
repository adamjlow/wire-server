// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestDeletionCodeOrderingAndFailure(t *testing.T) {
	ctx := context.Background()
	email := "synthetic@example.com"
	profile := selfProfile{Email: &email, Name: "Synthetic <Owner>", Locale: "en"}
	account := gocql.TimeUUID()
	var steps []string
	var row *deletionVerificationCode
	var delay *int32
	mailFailure := errors.New("synthetic delivery failure")
	failMail := false
	s := accountDeletionCodes{
		lookup: func(context.Context, string) (*deletionVerificationCode, error) {
			steps = append(steps, "lookup")
			return row, nil
		},
		throttle: func(context.Context, string) (*int32, error) { steps = append(steps, "throttle"); return delay, nil },
		insertThrottle: func(_ context.Context, _ string, ttl int32) error {
			steps = append(steps, "insert-throttle")
			if ttl != 60 {
				t.Fatal("throttle TTL")
			}
			return nil
		},
		insert: func(_ context.Context, code deletionVerificationCode) error {
			steps = append(steps, "insert")
			row = &code
			return nil
		},
		remove: func(context.Context, string) error { steps = append(steps, "remove"); row = nil; return nil },
		mail: func(_ context.Context, p selfProfile, key, code string) error {
			steps = append(steps, "mail")
			if p.Name != profile.Name || len(key) != 20 || len(code) != 20 || row.account == nil || *row.account != account || row.retries != 3 || row.ttl != 600 {
				t.Fatal("mail/code fields")
			}
			if failMail {
				return mailFailure
			}
			return nil
		},
		throttleTTL: 60,
	}
	ttl, err := s.send(ctx, profile, account)
	if err != nil || ttl != 600 || !reflect.DeepEqual(steps, []string{"lookup", "throttle", "insert-throttle", "insert", "mail"}) {
		t.Fatalf("send: %d %v %v", ttl, err, steps)
	}
	row.retries = 0
	row.ttl = 83
	steps = nil
	_, err = s.send(ctx, profile, account)
	var pending deletionCodePending
	if !errors.As(err, &pending) || pending.ttl != 83 || !reflect.DeepEqual(steps, []string{"lookup"}) {
		t.Fatalf("pending exhausted: %v %v", err, steps)
	}
	row = nil
	steps = nil
	n := int32(19)
	delay = &n
	_, err = s.send(ctx, profile, account)
	var throttled deletionCodeThrottled
	if !errors.As(err, &throttled) || throttled.ttl != 19 || !reflect.DeepEqual(steps, []string{"lookup", "throttle"}) {
		t.Fatalf("throttle: %v %v", err, steps)
	}
	delay = nil
	steps = nil
	failMail = true
	_, err = s.send(ctx, profile, account)
	if !errors.Is(err, mailFailure) || row != nil || !reflect.DeepEqual(steps, []string{"lookup", "throttle", "insert-throttle", "insert", "mail", "remove"}) {
		t.Fatalf("mail cleanup: %v %v", err, steps)
	}
}

func TestDeletionCodeVerificationRetainsRemainingTTL(t *testing.T) {
	row := deletionVerificationCode{key: "synthetic-key", value: "synthetic-code", email: "synthetic@example.com", ttl: 127, retries: 3}
	writes := 0
	s := accountDeletionCodes{
		lookup: func(context.Context, string) (*deletionVerificationCode, error) { copy := row; return &copy, nil },
		insert: func(_ context.Context, updated deletionVerificationCode) error { row = updated; writes++; return nil },
	}
	ctx := context.Background()
	for remaining := uint8(2); ; remaining-- {
		got, err := s.verify(ctx, row.key, "wrong")
		if err != nil || got != nil || row.retries != remaining || row.ttl != 127 {
			t.Fatalf("retry %d: %+v %v", remaining, row, err)
		}
		if remaining == 0 {
			break
		}
	}
	got, err := s.verify(ctx, row.key, row.value)
	if err != nil || got != nil || writes != 3 {
		t.Fatal("exhausted code accepted or rewritten")
	}
	row.retries = 1
	got, err = s.verify(ctx, row.key, row.value)
	if err != nil || got == nil || writes != 3 || row.retries != 1 {
		t.Fatal("successful verification consumed code")
	}
}

func TestDeletionMailNativeTemplates(t *testing.T) {
	r, err := newRegistrationMailRenderer(nativeMailTestConfig(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	email := "synthetic@example.com"
	m, err := r.deletion(selfProfile{Email: &email, Name: "Synthetic <Owner>", Locale: "en"}, "synthetic-key", "synthetic-code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.plain, "https://example.com/d/?key=synthetic-key&code=synthetic-code") || !strings.Contains(m.html, "key=synthetic-key&amp;code=synthetic-code") || strings.Contains(m.html, "Synthetic <Owner>") {
		t.Fatal("deletion URL/escaping")
	}
	if !reflect.DeepEqual(m.headers, [][2]string{{"X-Zeta-Purpose", "Delete"}, {"X-Zeta-Key", "synthetic-key"}, {"X-Zeta-Code", "synthetic-code"}}) {
		t.Fatal("native processing headers")
	}
}
