// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/url"
	"sync"
	"time"
)

type sqsFeedbackMessage struct {
	Body    *string `xml:"Body"`
	Receipt *string `xml:"ReceiptHandle"`
}

type sesFeedbackEvent struct {
	block     bool
	addresses []string
}

func decodeSESFeedback(body string) (sesFeedbackEvent, error) {
	var event sesFeedbackEvent
	invalid := errors.New("invalid SES feedback event")
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	var raw json.RawMessage
	var trailing any
	if decoder.Decode(&raw) != nil || decoder.Decode(&trailing) != io.EOF || !validRegistrationJSONText(raw) {
		return event, invalid
	}
	fields, err := registrationObject(raw)
	if err != nil {
		return event, invalid
	}
	kind, err := registrationText(fields, "notificationType", true)
	if err != nil {
		return event, invalid
	}
	container, recipients := "", ""
	switch *kind {
	case "Bounce":
		container, recipients = "bounce", "bouncedRecipients"
	case "Complaint":
		container, recipients, event.block = "complaint", "complainedRecipients", true
	default:
		return event, invalid
	}
	nested, err := registrationObject(fields[container])
	if err != nil {
		return event, invalid
	}
	if *kind == "Bounce" {
		bounce, err := registrationText(nested, "bounceType", true)
		if err != nil {
			return event, invalid
		}
		switch *bounce {
		case "Permanent":
			event.block = true
		case "Transient", "Undetermined":
		default:
			return event, invalid
		}
	}
	var rows []json.RawMessage
	if bytes.Equal(bytes.TrimSpace(nested[recipients]), []byte("null")) || json.Unmarshal(nested[recipients], &rows) != nil {
		return event, invalid
	}
	for _, row := range rows {
		object, err := registrationObject(row)
		if err != nil {
			return event, invalid
		}
		mailbox, err := registrationText(object, "emailAddress", true)
		if err != nil {
			return event, invalid
		}
		address, err := sesMailboxAddress(*mailbox)
		if err != nil {
			return event, invalid
		}
		event.addresses = append(event.addresses, address)
	}
	return event, nil
}

type sesFeedbackListener struct {
	queue    *userJournal
	block    func(context.Context, string) error
	throttle time.Duration
	log      func(string)
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
}

func startSESFeedback(ctx context.Context, queue *userJournal, store blocklistStore, throttle *int64) *sesFeedbackListener {
	if queue == nil {
		return nil
	}
	millis := int64(500)
	if throttle != nil {
		millis = *throttle
	}
	// Native cancels listeners after HTTP drain. Keep their own context alive
	// during serve's drain, then cancel/join through the deferred close.
	workerContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	l := &sesFeedbackListener{queue: queue, block: func(ctx context.Context, address string) error { return store.write(ctx, address, false) }, throttle: time.Duration(millis) * time.Millisecond, log: func(message string) { log.Print(message) }, ctx: workerContext, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(l.done); l.listen() }()
	return l
}

func (l *sesFeedbackListener) close() {
	if l != nil {
		l.cancel()
		<-l.done
	}
}

func feedbackWait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *sesFeedbackListener) listen() {
	for l.ctx.Err() == nil {
		response, _, err := l.queue.query(l.ctx, "ReceiveMessage", url.Values{"QueueUrl": {l.queue.queue}, "WaitTimeSeconds": {"20"}, "MaxNumberOfMessages": {"10"}})
		if err == nil {
			err = l.process(l.ctx, response.Messages)
		}
		if l.ctx.Err() != nil {
			return
		}
		if err != nil {
			l.log("SES feedback read or callback failed")
			if !feedbackWait(l.ctx, 3*time.Second) {
				return
			}
		} else if len(response.Messages) == 0 && !feedbackWait(l.ctx, l.throttle) {
			return
		}
	}
}

func (l *sesFeedbackListener) process(ctx context.Context, messages []sqsFeedbackMessage) error {
	batch, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	var mutex sync.Mutex
	next := 0
	var failure error
	for range min(10, len(messages)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				mutex.Lock()
				if next == len(messages) || batch.Err() != nil {
					mutex.Unlock()
					return
				}
				message := messages[next]
				next++
				mutex.Unlock()
				if err := l.message(batch, message); err != nil {
					mutex.Lock()
					if failure == nil {
						failure = err
						cancel()
					}
					mutex.Unlock()
					return
				}
			}
		}()
	}
	workers.Wait()
	if failure != nil {
		return failure
	}
	return ctx.Err()
}

func (l *sesFeedbackListener) message(ctx context.Context, message sqsFeedbackMessage) error {
	if message.Body == nil {
		l.log("SES feedback body missing")
		return nil
	}
	event, err := decodeSESFeedback(*message.Body)
	if err != nil {
		l.log("SES feedback parse failed")
		return nil
	}
	for _, address := range event.addresses {
		// Native logs each recipient before its possible block-list write.
		// Do not put addresses, message bodies or receipt handles in diagnostics.
		l.log("SES feedback recipient processed")
		if event.block {
			if err := l.block(ctx, address); err != nil {
				return err
			}
		}
	}
	if message.Receipt != nil {
		_, _, err := l.queue.query(ctx, "DeleteMessage", url.Values{"QueueUrl": {l.queue.queue}, "ReceiptHandle": {*message.Receipt}})
		return err
	}
	return nil
}
