// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestMLSRemoteSetValidationUsesNativeOrder(t *testing.T) {
	user := gocql.TimeUUID()
	client := federationTestPeer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api-version") {
			_, _ = io.WriteString(w, `{"supported":[0],"supported_versions":[4]}`)
			return
		}
		// Higher client first would produce an invalid-signature error if
		// validation ran in input order. Native builds its Set first.
		high := mlsBundleEntry{User: user.String(), Domain: "remote.example.com", Client: "b", Ref: "AQ==", Data: base64.StdEncoding.EncodeToString(syntheticMLSCountPackage(0, 101, 1))}
		low := mlsBundleEntry{User: user.String(), Domain: "remote.example.com", Client: "a", Ref: "AQ==", Data: "AA=="}
		_ = json.NewEncoder(w).Encode(mlsBundle{Entries: []mlsBundleEntry{high, low, low}})
	})
	s := mlsClientReads{remote: &client, domain: "example.com"}
	_, err := s.claimRemote(context.Background(), user, user, "remote.example.com", 1, "synthetic")
	var failure registrationAdmissionError
	if !errors.As(err, &failure) || failure.Code != 409 || failure.Label != "decoding-error" {
		t.Fatalf("native remote validation priority changed: %v", err)
	}
}

func TestMLSClaimAdmissionSelfSkipAndConsumeOnce(t *testing.T) {
	user := gocql.TimeUUID()
	team := gocql.TimeUUID()
	status := int32(0)
	trace := []string{}
	account := &storedUser{ID: user, Activated: true, Status: &status, Team: &team}
	lh := "enabled"
	s := mlsClientReads{domain: "example.com", now: func() time.Time { return time.Unix(100, 0) }, claimGate: make(chan struct{}, 1), user: func(context.Context, gocql.UUID) (*storedUser, error) {
		trace = append(trace, "user")
		return account, nil
	}, legalhold: func(context.Context, gocql.UUID, gocql.UUID, string) (string, error) {
		trace = append(trace, "legalhold")
		return lh, nil
	}, clients: func(context.Context, gocql.UUID) ([]storedClient, error) {
		trace = append(trace, "clients")
		return []storedClient{{ID: "b"}, {ID: "a"}}, nil
	}, guard: func(context.Context, gocql.UUID, map[string][]string, string) error {
		trace = append(trace, "guard")
		return nil
	}, packages: func(_ context.Context, _ gocql.UUID, client string, _ uint16) ([]mlsPackageRow, error) {
		trace = append(trace, "packages:"+client)
		return []mlsPackageRow{{ref: []byte{1}, data: syntheticMLSCountPackage(0, 101, 1)}, {ref: []byte{2}, data: []byte{0}}}, nil
	}, remove: func(context.Context, gocql.UUID, string, uint16, [][]byte) error {
		trace = append(trace, "expire")
		return nil
	}, removeOne: func(context.Context, gocql.UUID, string, uint16, []byte) error {
		trace = append(trace, "consume")
		return nil
	}}
	if _, err := s.claimLocal(context.Background(), user, "example.com", user, nil, 1, ""); !errors.Is(err, errMLSLegalhold) || strings.Join(trace, ",") != "user,legalhold" {
		t.Fatal("legalhold admission must precede client storage")
	}
	trace = nil
	status = 1
	if _, err := s.claimLocal(context.Background(), user, "example.com", user, nil, 1, ""); !errors.Is(err, errMLSInvalidUser) || strings.Join(trace, ",") != "user" {
		t.Fatal("suspended target consumed package")
	}
	trace = nil
	status = 3
	account.Activated = false
	lh = "disabled"
	skip := "a"
	result, err := s.claimLocal(context.Background(), user, "example.com", user, &skip, 1, "")
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Client != "b" || strings.Join(trace, ",") != "user,legalhold,clients,guard,packages:b,expire,consume" {
		t.Fatalf("native claim order: %v %+v %v", trace, result, err)
	}
	trace = nil
	_, err = s.claimLocal(context.Background(), user, "remote.example.com", user, &skip, 1, "")
	if err != nil || strings.Contains(strings.Join(trace, ","), "guard") || !strings.Contains(strings.Join(trace, ","), "packages:a") {
		t.Fatal("remote claimant inherited local self/consent rules")
	}
}

func TestMLSProcessGatePreventsDuplicateClaimAndCancelsWait(t *testing.T) {
	user := gocql.TimeUUID()
	remaining := true
	consumed := 0
	s := mlsClientReads{claimGate: make(chan struct{}, 1), now: func() time.Time { return time.Unix(100, 0) }, packages: func(context.Context, gocql.UUID, string, uint16) ([]mlsPackageRow, error) {
		if !remaining {
			return nil, nil
		}
		return []mlsPackageRow{{ref: []byte{1}, data: syntheticMLSCountPackage(0, 101, 1)}}, nil
	}, remove: func(context.Context, gocql.UUID, string, uint16, [][]byte) error { return nil }, removeOne: func(context.Context, gocql.UUID, string, uint16, []byte) error {
		remaining = false
		consumed++
		return nil
	}}
	var group sync.WaitGroup
	results := make(chan *mlsPackageRow, 2)
	for range 2 {
		group.Go(func() {
			row, err := s.claimOne(context.Background(), user, "a", 1)
			if err != nil {
				t.Error(err)
			}
			results <- row
		})
	}
	group.Wait()
	close(results)
	count := 0
	for row := range results {
		if row != nil {
			count++
		}
	}
	if count != 1 || consumed != 1 {
		t.Fatal("same package claimed twice")
	}
	s.claimGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.claimOne(ctx, user, "a", 1); !errors.Is(err, context.Canceled) {
		t.Fatal("waiting claim ignored cancellation")
	}
	<-s.claimGate
}

func TestMLSIncomingFederationDisabledAndAdmissionNull(t *testing.T) {
	s := mlsClientReads{domain: "example.com", user: func(context.Context, gocql.UUID) (*storedUser, error) { return nil, nil }}
	for _, tc := range []struct {
		enabled bool
		suite   string
		status  int
		body    string
	}{{false, "3", 200, "null"}, {true, "1", 200, "null"}, {true, "3", 400, ""}} {
		s.enabled = tc.enabled
		req := httptest.NewRequest("POST", "/federation/claim-key-packages", strings.NewReader(`{"claimant":"`+gocql.TimeUUID().String()+`","target":"`+gocql.TimeUUID().String()+`","cipher_suite":`+tc.suite+`}`))
		req.Header.Set("Wire-Origin-Domain", "remote.example.com")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if !s.handle(w, req, "/federation/claim-key-packages") || w.Code != tc.status {
			t.Fatal("native federation feature/admission status changed")
		}
		if tc.body != "" && strings.TrimSpace(w.Body.String()) != tc.body {
			t.Fatal("native federation rejection should be null")
		}
	}
}
