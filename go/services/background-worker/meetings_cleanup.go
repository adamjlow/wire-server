// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"context"
	"errors"
	"time"
)

type meetingsCleanupStore interface {
	OldMeetings(context.Context, time.Time, int) ([]storedMeeting, error)
	DeleteMeetingCode(context.Context, string) error
	DeleteMeeting(context.Context, string) error
}
type meetingsConversationEffects interface {
	Conversation(context.Context, string) (*storedConversation, error)
	DeleteLocalConversationUnchecked(context.Context, string) error
}

// Native forceDeleteMeeting uses no user auth checks or notification path; its
// internal unchecked deletion performs source storage/proposal/code effects.
func cleanupMeetingsBatch(ctx context.Context, store meetingsCleanupStore, conversations meetingsConversationEffects, cutoff time.Time, batch int) (int64, error) {
	if store == nil || conversations == nil {
		return 0, errors.New("meeting cleanup dependencies unavailable")
	}
	meetings, err := store.OldMeetings(ctx, cutoff, batch)
	if err != nil {
		return 0, err
	}
	for _, meeting := range meetings {
		convID := postgresUUIDString(meeting.Conversation)
		conv, err := conversations.Conversation(ctx, convID)
		if err != nil {
			return 0, err
		}
		if conv != nil && conv.ID == convID {
			kind, ok := conv.Metadata["group_conv_type"].(*int32)
			if ok && kind != nil && *kind == 2 {
				// The unchecked subsystem rereads the conversation, so a concurrent
				// disappearance is an error rather than a silently successful deletion.
				if err := conversations.DeleteLocalConversationUnchecked(ctx, convID); err != nil {
					return 0, err
				}
			}
		}
		id := postgresUUIDString(meeting.ID)
		if err := store.DeleteMeetingCode(ctx, id); err != nil {
			return 0, err
		}
		if err := store.DeleteMeeting(ctx, id); err != nil {
			return 0, err
		}
	}
	return int64(len(meetings)), nil
}

type meetingsCleanupResult struct {
	TotalDeleted int64
	Completed    bool // Native increments runs even after a caught subsystem error.
	BatchError   error
}

// The runtime computes the retention cutoff once per run. Every full batch uses
// that same cutoff; partial batches stop. Partial effects in a failed batch are
// not included in totalDeleted, matching the native batch result boundary.
func runMeetingsCleanup(ctx context.Context, store meetingsCleanupStore, conversations meetingsConversationEffects, cutoff time.Time, batch int, report func(int64, int64, error)) (meetingsCleanupResult, error) {
	var result meetingsCleanupResult
	if batch <= 0 {
		return result, errors.New("invalid meeting cleanup batch size")
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		deleted, err := cleanupMeetingsBatch(ctx, store, conversations, cutoff, batch)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			if report != nil {
				report(deleted, result.TotalDeleted, err)
			}
			result.BatchError = err
			result.Completed = true
			return result, nil
		}
		result.TotalDeleted += deleted
		if report != nil {
			report(deleted, result.TotalDeleted, nil)
		}
		if deleted < int64(batch) {
			result.Completed = true
			return result, nil
		}
	}
}

// Source runCleanupOldMeetings snapshots Now before the first batch. A cutoff
// subsystem error completes the run with its caught error boundary;
// malformed configuration and cancellation remain startup/handler errors.
func runConfiguredMeetingsCleanup(ctx context.Context, config MeetingsCleanupConfig, store meetingsCleanupStore, conversations meetingsConversationEffects, now time.Time, report func(int64, int64, error)) (meetingsCleanupResult, error) {
	if err := config.validate(); err != nil {
		return meetingsCleanupResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return meetingsCleanupResult{}, err
	}
	cutoff, err := meetingRetentionCutoff(now, config.RetentionHours)
	if err != nil {
		return meetingsCleanupResult{Completed: true, BatchError: err}, nil
	}
	return runMeetingsCleanup(ctx, store, conversations, cutoff, config.BatchSize, report)
}
