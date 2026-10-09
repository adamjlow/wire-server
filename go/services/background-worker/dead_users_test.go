// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/cassandra-gocql-driver/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

type observedMissedStore struct {
	calls  int
	user   gocql.UUID
	client string
	fail   bool
	acks   *acknowledgements
}

func (s *observedMissedStore) MarkMissed(_ context.Context, user gocql.UUID, client string) error {
	s.calls++
	s.user = user
	s.client = client
	if s.acks.acks != 0 {
		return errors.New("ack preceded storage write")
	}
	if s.fail {
		return errors.New("synthetic storage failure")
	}
	return nil
}

func TestDeadUserDeliveryWritesBeforeACKAndNormalizesClient(t *testing.T) {
	for _, mode := range []uint8{0, amqp.Persistent} {
		acks := &acknowledgements{}
		store := &observedMissedStore{acks: acks}
		d := amqp.Delivery{DeliveryMode: mode, DeliveryTag: 1, Acknowledger: acks, Headers: amqp.Table{"x-last-death-queue": "user-notifications.11111111-1111-1111-1111-111111111111.00000000000000000000000Ab"}}
		if err := handleDeadUser(t.Context(), store, d); err != nil {
			t.Fatal(err)
		}
		if store.calls != 1 || store.client != "ab" || store.user.String() != "11111111-1111-1111-1111-111111111111" || acks.acks != 1 || len(acks.rejects) != 0 {
			t.Fatal("persistent DLQ storage/ACK contract changed")
		}
	}
}

func TestTransientDeadUserDeliveryDoesNotRequireHeadersOrStorage(t *testing.T) {
	acks := &acknowledgements{}
	store := &observedMissedStore{acks: acks, fail: true}
	if err := handleDeadUser(t.Context(), store, amqp.Delivery{DeliveryMode: amqp.Transient, DeliveryTag: 1, Acknowledger: acks}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 0 || acks.acks != 1 {
		t.Fatal("transient notification wrote missed state")
	}
}

func TestDeadUserMalformedHeadersAndWriteFailureRemainUnacknowledged(t *testing.T) {
	valid := "user-notifications.11111111-1111-1111-1111-111111111111.a"
	for _, header := range []any{nil, []byte(valid), "wrong", valid + ".extra", "user-notifications.not-a-uuid.a", "user-notifications.11111111-1111-1111-1111-111111111111.10000000000000000", "user-notifications.11111111-1111-1111-1111-111111111111.';DROP TABLE missed_notifications;--", valid} {
		acks := &acknowledgements{}
		store := &observedMissedStore{acks: acks, fail: true}
		d := amqp.Delivery{DeliveryMode: amqp.Persistent, DeliveryTag: 1, Acknowledger: acks, Headers: amqp.Table{"x-last-death-queue": header}}
		if err := handleDeadUser(t.Context(), store, d); err == nil {
			t.Fatal("bad header/storage failure accepted")
		}
		if acks.acks != 0 || len(acks.rejects) != 0 {
			t.Fatal("failed DLQ notification removed from queue")
		}
		if header != valid && store.calls != 0 {
			t.Fatal("malformed queue header reached storage")
		}
	}
}
