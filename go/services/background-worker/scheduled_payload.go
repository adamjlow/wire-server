// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"errors"
	"time"
)

// Wire.API.Jobs persists {type,data} in Arbiter, independently of the Rabbit
// background-job envelope. Queue identity constrains which tags are executable.
type scheduledJobPayload struct {
	Type                 string
	Team                 string
	Conversation         string
	Actor                *string
	RequestID            string
	DeletionScheduledFor *time.Time
}

func decodeScheduledPayload(queue string, body []byte) (scheduledJobPayload, error) {
	invalid := errors.New("invalid scheduled job payload")
	var envelope struct {
		Type *string         `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	var result scheduledJobPayload
	if json.Unmarshal(body, &envelope) != nil || envelope.Type == nil {
		return result, invalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(envelope.Data, &object) != nil || object == nil {
		return result, invalid
	}
	result.Type = *envelope.Type
	if queue == "meetings" && result.Type == "meetings_cleanup" {
		return result, nil // Source empty object schema accepts extra fields.
	}
	if queue != "conversations" {
		return result, invalid
	}
	switch result.Type {
	case "adminless_setup", "adminless_deletion", "adminless_reminder":
	default:
		return result, invalid
	}
	// Decode only fields belonging to this tag. An extra field used by a
	// different constructor is unknown to the native schema and is ignored.
	readText := func(name string, required bool) (*string, error) {
		var value *string
		raw := object[name]
		if raw == nil {
			if required {
				return nil, invalid
			}
			return nil, nil
		}
		if json.Unmarshal(raw, &value) != nil || required && value == nil {
			return nil, invalid
		}
		return value, nil
	}
	teamText, err := readText("team_id", true)
	if err != nil {
		return result, err
	}
	requestID, err := readText("request_id", true)
	if err != nil {
		return result, err
	}
	actorText, err := readText("orig_user_id", false)
	if err != nil {
		return result, err
	}
	team, err := parseUserID(*teamText)
	if err != nil {
		return result, invalid
	}
	result.Team, result.RequestID = team.String(), *requestID
	if actorText != nil {
		actor, err := parseUserID(*actorText)
		if err != nil {
			return result, invalid
		}
		result.Actor = stringPtr(actor.String())
	}
	if result.Type != "adminless_setup" {
		convText, err := readText("conversation_id", true)
		if err != nil {
			return result, err
		}
		conv, err := parseUserID(*convText)
		if err != nil {
			return result, invalid
		}
		result.Conversation = conv.String()
	}
	if result.Type == "adminless_reminder" {
		timeText, err := readText("deletion_scheduled_for", true)
		if err != nil {
			return result, err
		}
		when, err := time.Parse(time.RFC3339Nano, *timeText)
		if err != nil {
			return result, invalid
		}
		when = when.UTC().Truncate(time.Millisecond)
		result.DeletionScheduledFor = &when
	}
	return result, nil
}
