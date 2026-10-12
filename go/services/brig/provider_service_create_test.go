// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
)

func TestNewServiceAdmission(t *testing.T) {
	base := map[string]any{"name": "Synthetic", "summary": "Synthetic summary", "description": "Synthetic description", "base_url": "https://example.com/service", "public_key": "-----BEGIN PUBLIC KEY-----\nAQID\n-----END PUBLIC KEY-----\n", "assets": []any{}, "tags": []string{"business", "audio", "audio"}}
	raw, _ := json.Marshal(base)
	request, err := decodeProviderNewService(raw)
	if err != nil || !reflect.DeepEqual(request.tags, []int64{1, 4}) {
		t.Fatal("native set/schema rejected", err)
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"name", ""}, {"summary", strings.Repeat("界", 129)}, {"description", ""}, {"base_url", "http://example.com"}, {"assets", nil}, {"tags", []string{}}, {"tags", []string{"audio", "books", "business", "design"}}, {"public_key", "invalid"}, {"auth_token", "not valid!"}} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.field] = tc.value
		raw, _ := json.Marshal(copy)
		if _, err := decodeProviderNewService(raw); err == nil {
			t.Fatal("invalid service accepted", tc.field)
		}
	}
	base["auth_token"] = ""
	raw, _ = json.Marshal(base)
	request, err = decodeProviderNewService(raw)
	if err != nil || request.token == nil || *request.token != "" {
		t.Fatal("native optional empty token changed")
	}
	token, err := newServiceToken()
	decoded, decodeErr := base64.URLEncoding.DecodeString(token)
	if err != nil || decodeErr != nil || len(token) != 24 || len(decoded) != 18 {
		t.Fatal("native generated token shape changed")
	}
}

func TestNativeServiceRSAAdmissionAndCancellation(t *testing.T) {
	worker := &passwordService{slots: make(chan struct{}, 1)}
	strong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&strong.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	size, fingerprint, err := worker.serviceKey(context.Background(), encoded)
	if err != nil || size != 2048 || len(fingerprint) != 32 {
		t.Fatal("native RSA key rejected", err)
	}
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	weakDER, _ := x509.MarshalPKIXPublicKey(&weak.PublicKey)
	edPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edDER, _ := x509.MarshalPKIXPublicKey(edPublic)
	for _, bad := range [][]byte{pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: weakDER}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: edDER}), []byte("invalid"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-256-CBC,00000000000000000000000000000000"}, Bytes: make([]byte, 16)})} {
		if _, _, err := worker.serviceKey(context.Background(), bad); !errors.Is(err, errServiceKeyInvalid) {
			t.Fatal("invalid native key accepted", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.slots <- struct{}{}
	if _, _, err := worker.serviceKey(ctx, encoded); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled key work ran")
	}
	<-worker.slots
}

func TestServiceCreateNativeEffectOrder(t *testing.T) {
	id, _ := gocql.ParseUUID("11111111-2222-3333-4444-555555555555")
	for _, tc := range []struct {
		name                       string
		missing, generated, badKey bool
		steps                      []string
	}{
		{"supplied", false, false, false, []string{"account", "key", "insert"}},
		{"generated", false, true, false, []string{"account", "key", "token", "insert"}},
		{"missing account", true, false, true, []string{"account"}},
		{"invalid key", false, true, true, []string{"account", "key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []string
			s := providerServiceCreation{account: func(context.Context, gocql.UUID) (*providerAccount, error) {
				steps = append(steps, "account")
				if tc.missing {
					return nil, nil
				}
				return &providerAccount{}, nil
			}, key: func(context.Context, []byte) (int32, []byte, error) {
				steps = append(steps, "key")
				if tc.badKey {
					return 0, nil, errServiceKeyInvalid
				}
				return 2048, []byte("synthetic fingerprint"), nil
			}, newToken: func() (string, error) { steps = append(steps, "token"); return "synthetic", nil }, insert: func(_ context.Context, provider gocql.UUID, r providerNewService, token string, size int32, fp []byte) (gocql.UUID, error) {
				steps = append(steps, "insert")
				if provider != id || token != "synthetic" || size != 2048 || string(fp) != "synthetic fingerprint" {
					t.Fatal("wrong stored key/token")
				}
				return id, nil
			}}
			token := "synthetic"
			r := providerNewService{token: &token}
			if tc.generated {
				r.token = nil
			}
			reply, err := s.create(context.Background(), id, r)
			if !reflect.DeepEqual(steps, tc.steps) {
				t.Fatal("changed creation order", steps)
			}
			if tc.missing {
				if !errors.Is(err, errProviderDeletionInvalid) {
					t.Fatal(err)
				}
			} else if tc.badKey {
				if !errors.Is(err, errServiceKeyInvalid) {
					t.Fatal(err)
				}
			} else if err != nil || reply.ID != id || (reply.Token != nil) != tc.generated {
				t.Fatal("wrong creation reply", err)
			}
		})
	}
}
