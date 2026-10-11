// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
)

type internalEventsConfig struct {
	Type string `yaml:"queueType"`
	Name string `yaml:"queueName"`
}

type internalEventQueue struct{ sqs *userJournal }

func newInternalEventQueue(ctx context.Context, config *internalEventsConfig, aws *brigAWSConfig, client *http.Client) (*internalEventQueue, error) {
	if config == nil || config.Type != "sqs" {
		// STOMP transport/listener remains required before whole-service release.
		return nil, errors.New("internal event queue transport unavailable")
	}
	sqs, err := newBrigSQS(ctx, aws, config.Name, client)
	if err != nil {
		return nil, err
	}
	return &internalEventQueue{sqs: sqs}, nil
}

func (q *internalEventQueue) enqueueUserDeletion(ctx context.Context, user gocql.UUID) error {
	if q == nil || q.sqs == nil {
		return errors.New("internal event queue unavailable")
	}
	body, err := json.Marshal(struct {
		User string `json:"user"`
		Type string `json:"type"`
	}{User: user.String(), Type: "user.delete"})
	if err != nil {
		return errors.New("internal event encoding failed")
	}
	// MD5 is the native SQS response integrity check, not password hashing.
	digest := md5.Sum(body)
	params := url.Values{"QueueUrl": {q.sqs.queue}, "MessageBody": {string(body)}}
	for attempt := 0; ; attempt++ {
		result, retry, err := q.sqs.query(ctx, "SendMessage", params)
		if err == nil {
			if result.MD5 == nil || *result.MD5 != hex.EncodeToString(digest[:]) {
				return errors.New("internal event response integrity failed")
			}
			return nil
		}
		if !retry || attempt >= 5 || ctx.Err() != nil {
			return err
		}
		if waitBrigRetry(ctx, 100*time.Millisecond<<attempt) != nil {
			return errors.New("internal event request canceled")
		}
	}
}
