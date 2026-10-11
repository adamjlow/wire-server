// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type registrationClientFake struct {
	clients  []storedClient
	events   *[]string
	written  storedClient
	keys     []uncheckedPrekey
	writeErr error
}

func (f *registrationClientFake) read(context.Context, gocql.UUID, *string) ([]storedClient, error) {
	*f.events = append(*f.events, "lookup")
	return f.clients, nil
}
func (f *registrationClientFake) insertClient(_ context.Context, _ gocql.UUID, c storedClient, keys []uncheckedPrekey) error {
	*f.events = append(*f.events, "insert")
	f.written, f.keys = c, keys
	return f.writeErr
}

func TestClientRegistrationAdmissionPrecedence(t *testing.T) {
	key := uncheckedPrekey{ID: 7, Key: "pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY="}
	denied := errors.New("reauthentication denied")
	for _, tc := range []struct {
		name          string
		clients       []storedClient
		typ           int32
		last          uncheckedPrekey
		authErr, want error
		events        []string
	}{
		{"auth before capacity and key", []storedClient{{ID: "a", Type: 1}}, 1, uncheckedPrekey{}, denied, denied, []string{"lookup", "auth"}},
		{"capacity before key", []storedClient{{ID: "a", Type: 1}}, 1, uncheckedPrekey{}, nil, errClientCapacity, []string{"lookup", "auth"}},
		{"same ID upsert bypasses auth and capacity", []storedClient{{ID: clientIDFromPrekey(key), Type: 1}}, 1, key, denied, nil, []string{"lookup", "time", "insert"}},
		{"other type does not require auth", []storedClient{{ID: "a", Type: 1}}, 0, key, denied, nil, []string{"lookup", "time", "insert"}},
		{"temporary replacement still requires auth", []storedClient{{ID: "a", Type: 0}}, 0, key, denied, denied, []string{"lookup", "auth"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{}
			store := &registrationClientFake{clients: tc.clients, events: &events}
			core := clientRegistrationCore{store: store, maxPermanent: 1,
				now:            func() time.Time { events = append(events, "time"); return time.Unix(1, 123456789) },
				reauthenticate: func(context.Context, gocql.UUID, *string) error { events = append(events, "auth"); return tc.authErr },
			}
			_, _, _, err := core.upsert(context.Background(), gocql.UUID{}, clientRegistrationRequest{client: storedClient{Type: tc.typ}, last: tc.last})
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(events, tc.events) {
				t.Fatalf("error=%v events=%v", err, events)
			}
		})
	}
}

func TestClientRegistrationReplacementAndPartialFailure(t *testing.T) {
	key := uncheckedPrekey{ID: 7, Key: "pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY="}
	events := []string{}
	store := &registrationClientFake{clients: []storedClient{{ID: "a", Type: 0}, {ID: "b", Type: 1}}, events: &events}
	core := clientRegistrationCore{store: store, maxPermanent: 1, now: func() time.Time { return time.Unix(1, 123456789) }, reauthenticate: func(context.Context, gocql.UUID, *string) error { return nil }}
	request := clientRegistrationRequest{client: storedClient{Type: 0, PublicKeys: map[string][]byte{"ed25519": {1}}}, last: key, prekeys: []uncheckedPrekey{key}}
	client, old, total, err := core.upsert(context.Background(), gocql.UUID{}, request)
	if err != nil || total != 3 || len(old) != 1 || old[0].ID != "a" || client.LastActive != nil || client.Created.Nanosecond() != 123000000 || !reflect.DeepEqual(store.keys, []uncheckedPrekey{key, key}) {
		t.Fatalf("unexpected result: total=%d old=%v error=%v", total, old, err)
	}
	store.writeErr = errClientMLSDuplicate
	_, _, _, err = core.upsert(context.Background(), gocql.UUID{}, request)
	if !errors.Is(err, errClientMLSDuplicate) || store.written.ID != clientIDFromPrekey(key) {
		t.Fatal("must propagate duplicate without compensating committed writes")
	}
	request.prekeys = []uncheckedPrekey{{ID: 8, Key: key.Key}}
	events = nil
	_, _, _, err = core.upsert(context.Background(), gocql.UUID{}, request)
	if !errors.Is(err, errPrekeyBundle) || !reflect.DeepEqual(events, []string{"lookup"}) {
		t.Fatalf("invalid later prekey must prevent all writes: %v %v", err, events)
	}
}

func TestClientRegistrationInternalReauthenticationPolicy(t *testing.T) {
	key := uncheckedPrekey{ID: 7, Key: "pQABAQcCoQBYIDXdN8VlKb5lbgPmoDPLPyqNIEyShG4oT/DlW0peRRZUA6EAoQBYILLf1TIwSB62q69Ojs/X1tzJ+dYHNAw4QbW/7TC5vSZqBPY="}
	events := []string{}
	store := &registrationClientFake{clients: []storedClient{{ID: "a", Type: 0}}, events: &events}
	core := clientRegistrationCore{store: store, maxPermanent: 7, now: time.Now, reauthPolicy: func(count int, exists bool) bool {
		if count != 1 || exists {
			t.Fatal("policy did not receive native same-type count/upsert")
		}
		return false
	}}
	if _, _, _, err := core.upsert(context.Background(), gocql.UUID{}, clientRegistrationRequest{client: storedClient{Type: 0}, last: key}); err != nil {
		t.Fatal("explicit internal policy ignored", err)
	}
}
