// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type internalDeletionEvent struct {
	kind, client            string
	user, provider, service gocql.UUID
	connection              *string
}

func (internalDeletionEvent) String() string { return "<InternalDeletionEvent>" }

func decodeInternalDeletionEvent(raw json.RawMessage) (internalDeletionEvent, error) {
	var event internalDeletionEvent
	invalid := errors.New("invalid internal deletion event")
	if !validRegistrationJSONText(raw) {
		return event, invalid
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return event, invalid
	}
	kind, err := registrationText(fields, "type", true)
	if err != nil {
		return event, invalid
	}
	event.kind = *kind
	uuid := func(key string) (gocql.UUID, error) {
		value, err := registrationText(fields, key, true)
		if err != nil {
			return gocql.UUID{}, invalid
		}
		return registrationUUID(*value)
	}
	switch event.kind {
	case "user.delete":
		event.user, err = uuid("user")
	case "client.delete":
		event.user, err = uuid("user")
		if err != nil {
			return event, invalid
		}
		client := fields["client"]
		if object, e := registrationObject(client); e == nil {
			client = object["id"]
		}
		var text string
		if json.Unmarshal(client, &text) != nil {
			return event, invalid
		}
		event.client, err = parseClientID(text)
		if err != nil {
			return event, invalid
		}
		// Native .: connection requires presence but accepts explicit null.
		if _, present := fields["connection"]; !present {
			return event, invalid
		}
		event.connection, err = registrationText(fields, "connection", false)
	case "service.delete":
		event.provider, err = uuid("provider")
		if err != nil {
			return event, invalid
		}
		event.service, err = uuid("service")
	default:
		return event, invalid
	}
	if err != nil {
		return event, invalid
	}
	return event, nil
}

type internalDeletionProcessor struct {
	get           func(context.Context, gocql.UUID) (*storedUser, error)
	deleteUser    func(context.Context, storedUser, string) error
	deleteClient  func(context.Context, gocql.UUID, string, *string, string) error
	deleteService func(context.Context, gocql.UUID, gocql.UUID) error
	throttle      time.Duration
}

func (p internalDeletionProcessor) process(ctx context.Context, event internalDeletionEvent) error {
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	switch event.kind {
	case "user.delete":
		if p.get == nil || p.deleteUser == nil {
			return errors.New("internal user deletion unavailable")
		}
		user, err := p.get(work, event.user)
		if err != nil {
			return err
		}
		if user != nil {
			if err := p.deleteUser(work, *user, ""); err != nil {
				return err
			}
		}
		if !feedbackWait(work, p.throttle) {
			return errors.New("internal deletion timed out or canceled")
		}
	case "client.delete":
		if p.deleteClient == nil {
			return errors.New("internal client deletion unavailable")
		}
		if err := p.deleteClient(work, event.user, event.client, event.connection, ""); err != nil {
			return err
		}
	case "service.delete":
		if p.deleteService == nil {
			return errors.New("internal service deletion unavailable")
		}
		if err := p.deleteService(work, event.provider, event.service); err != nil {
			return err
		}
	default:
		return errors.New("invalid internal deletion event")
	}
	return work.Err()
}

type internalEventListener struct {
	queue     *userJournal
	processor internalDeletionProcessor
	throttle  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
}

func startInternalEvents(ctx context.Context, queue *internalEventQueue, processor internalDeletionProcessor, throttle *int64) *internalEventListener {
	worker, cancel := context.WithCancel(context.WithoutCancel(ctx))
	millis := int64(500)
	if throttle != nil {
		millis = *throttle
	}
	l := &internalEventListener{queue: queue.sqs, processor: processor, throttle: time.Duration(millis) * time.Millisecond, ctx: worker, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(l.done); l.listen() }()
	return l
}

func (l *internalEventListener) close() {
	if l != nil {
		l.cancel()
		<-l.done
	}
}

func (l *internalEventListener) listen() {
	for l.ctx.Err() == nil {
		response, _, err := l.queue.query(l.ctx, "ReceiveMessage", url.Values{"QueueUrl": {l.queue.queue}, "WaitTimeSeconds": {"20"}, "MaxNumberOfMessages": {"10"}})
		if err == nil {
			err = processSQSBatch(l.ctx, response.Messages, l.message)
		}
		if l.ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Print("Internal event read or callback failed")
			if !feedbackWait(l.ctx, 3*time.Second) {
				return
			}
		} else if len(response.Messages) == 0 && !feedbackWait(l.ctx, l.throttle) {
			return
		}
	}
}

func (l *internalEventListener) message(ctx context.Context, message sqsFeedbackMessage) error {
	if message.Body == nil {
		log.Print("Internal event body missing")
		return nil
	}
	event, err := decodeInternalDeletionEvent(json.RawMessage(*message.Body))
	if err != nil {
		log.Print("Internal event parse failed")
		return nil
	}
	if err := l.processor.process(ctx, event); err != nil {
		return err
	}
	if message.Receipt != nil {
		_, _, err := l.queue.query(ctx, "DeleteMessage", url.Values{"QueueUrl": {l.queue.queue}, "ReceiptHandle": {*message.Receipt}})
		return err
	}
	return nil
}
