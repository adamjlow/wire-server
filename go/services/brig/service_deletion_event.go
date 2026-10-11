// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type deletionEventValue func(json.RawMessage) bool

func deletionEventObject(raw json.RawMessage) map[string]json.RawMessage {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func deletionEventFields(raw json.RawMessage, required, nullable, optional map[string]deletionEventValue) bool {
	value := deletionEventObject(raw)
	if value == nil {
		return false
	}
	for key, valid := range required {
		if !valid(value[key]) {
			return false
		}
	}
	for key, valid := range nullable {
		if data, found := value[key]; found && !bytes.Equal(bytes.TrimSpace(data), []byte("null")) && !valid(data) {
			return false
		}
	}
	for key, valid := range optional {
		if data, found := value[key]; found && !valid(data) {
			return false
		}
	}
	return true
}

func deletionEventText(raw json.RawMessage) bool {
	var text string
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &text) == nil
}

func deletionEventEnum(allowed ...string) deletionEventValue {
	return func(raw json.RawMessage) bool {
		if !deletionEventText(raw) {
			return false
		}
		var value string
		_ = json.Unmarshal(raw, &value)
		for _, text := range allowed {
			if value == text {
				return true
			}
		}
		return false
	}
}

func deletionEventBool(raw json.RawMessage) bool {
	return bytes.Equal(raw, []byte("true")) || bytes.Equal(raw, []byte("false"))
}

func deletionEventUUID(raw json.RawMessage) bool {
	var value string
	if !deletionEventText(raw) || json.Unmarshal(raw, &value) != nil {
		return false
	}
	_, ok := tokenUUID(value)
	return ok
}

func deletionEventQualified(raw json.RawMessage) bool {
	return deletionEventFields(raw, map[string]deletionEventValue{"id": deletionEventUUID, "domain": func(raw json.RawMessage) bool {
		var value string
		return deletionEventText(raw) && json.Unmarshal(raw, &value) == nil && deletionEventDomain(value)
	}}, nil, nil)
}

func deletionEventTime(raw json.RawMessage) bool {
	var value string
	if !deletionEventText(raw) || json.Unmarshal(raw, &value) != nil {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func deletionEventArray(item deletionEventValue) deletionEventValue {
	return func(raw json.RawMessage) bool {
		var values []json.RawMessage
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &values) != nil {
			return false
		}
		for _, value := range values {
			if !item(value) {
				return false
			}
		}
		return true
	}
}

func deletionEventInteger(bits int, signed bool) deletionEventValue {
	return func(raw json.RawMessage) bool {
		digits, valid := deletionEventIntegerText(raw)
		if !valid {
			return false
		}
		if signed {
			_, err := strconv.ParseInt(digits, 10, bits)
			return err == nil
		}
		_, err := strconv.ParseUint(digits, 10, bits)
		return err == nil
	}
}

func deletionEventIntegerText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == 'n' {
		return "", false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return "", false
	}
	text := number.String()
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	exponent := int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		var err error
		exponent, err = strconv.ParseInt(text[i+1:], 10, 64)
		if err != nil {
			return "", false
		}
		text = text[:i]
	}
	whole, fraction, _ := strings.Cut(text, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0", true
	}
	if exponent < -int64(len(digits)) || exponent > int64(len(fraction))+20 {
		return "", false
	}
	scale := exponent - int64(len(fraction))
	if scale < 0 {
		remove := -scale
		if remove >= int64(len(digits)) {
			return "", false
		}
		cut := len(digits) - int(remove)
		if strings.Trim(digits[cut:], "0") != "" {
			return "", false
		}
		digits = digits[:cut]
	} else {
		if int64(len(digits))+scale > 20 {
			return "", false
		}
		digits += strings.Repeat("0", int(scale))
	}
	if negative {
		digits = "-" + digits
	}
	return digits, true
}

func deletionEventBase64(raw json.RawMessage) bool {
	var value string
	if !deletionEventText(raw) || json.Unmarshal(raw, &value) != nil {
		return false
	}
	if strings.ContainsAny(value, "\r\n") {
		return false
	}
	_, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil
}

func deletionEventRole(raw json.RawMessage) bool {
	var role string
	if !deletionEventText(raw) || json.Unmarshal(raw, &role) != nil || len(role) < 2 || len(role) > 128 {
		return false
	}
	for _, ch := range role {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_') {
			return false
		}
	}
	return true
}

func deletionEventClient(raw json.RawMessage) bool {
	var id string
	if !deletionEventText(raw) || json.Unmarshal(raw, &id) != nil {
		return false
	}
	_, err := parseClientID(id)
	return err == nil
}

func validDeletionConversationEvent(raw []byte) bool {
	if len(raw) > 4<<20 || !utf8.Valid(raw) || !deletionEventFields(raw, map[string]deletionEventValue{"qualified_conversation": deletionEventQualified, "qualified_from": deletionEventQualified, "via": deletionEventEnum("user", "scim"), "time": deletionEventTime, "type": deletionEventText}, map[string]deletionEventValue{"team": deletionEventUUID, "subconv": deletionEventText}, nil) {
		return false
	}
	event := deletionEventObject(raw)
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	data, exists := event["data"]
	if !exists {
		return false
	}
	i32, u64 := deletionEventInteger(32, true), deletionEventInteger(64, false)
	required := map[string]deletionEventValue{}
	nullable := map[string]deletionEventValue{}
	switch kind {
	case "conversation.member-leave":
		required = map[string]deletionEventValue{"reason": deletionEventEnum("left", "user-deleted", "removed"), "qualified_user_ids": deletionEventArray(deletionEventQualified), "user_ids": deletionEventArray(deletionEventUUID)}
	case "conversation.member-join":
		required = map[string]deletionEventValue{"add_type": deletionEventEnum("internal_add", "external_add"), "users": deletionEventArray(func(raw json.RawMessage) bool {
			return deletionEventFields(raw, map[string]deletionEventValue{"qualified_id": deletionEventQualified}, nil, nil)
		})}
	case "conversation.member-update":
		required["qualified_target"] = deletionEventQualified
		nullable = map[string]deletionEventValue{"otr_muted_status": i32, "otr_muted_ref": deletionEventText, "otr_archived": deletionEventBool, "otr_archived_ref": deletionEventText, "hidden": deletionEventBool, "hidden_ref": deletionEventText, "conversation_role": deletionEventRole}
	case "conversation.rename":
		required["name"] = deletionEventText
	case "conversation.receipt-mode-update":
		required["receipt_mode"] = i32
	case "conversation.message-timer-update":
		nullable["message_timer"] = u64
	case "conversation.typing":
		required["status"] = deletionEventEnum("started", "stopped")
	case "conversation.connect-request":
		required["qualified_recipient"] = deletionEventQualified
		nullable = map[string]deletionEventValue{"name": deletionEventText, "message": deletionEventText, "email": deletionEventText}
	case "conversation.otr-message-add":
		required = map[string]deletionEventValue{"sender": deletionEventClient, "recipient": deletionEventClient, "text": deletionEventText}
		nullable["data"] = deletionEventText
	case "conversation.protocol-update":
		return deletionEventFields(data, nil, map[string]deletionEventValue{"protocol": deletionEventEnum("proteus", "mls", "mixed")}, nil)
	case "conversation.add-permission-update":
		required["add_permission"] = deletionEventEnum("admins", "everyone")
	case "conversation.adminless-reminder":
		required["deletion_scheduled_for"] = deletionEventTime
	case "conversation.mls-reset":
		required["group_id"] = deletionEventBase64
		nullable["new_group_id"] = deletionEventBase64
	case "conversation.mls-message-add", "conversation.mls-welcome":
		return deletionEventBase64(data)
	case "conversation.code-delete", "conversation.delete", "conversation.delete-meeting":
		return bytes.Equal(data, []byte("null"))
	case "conversation.code-update":
		required = map[string]deletionEventValue{"key": deletionEventCode(20, 20), "code": deletionEventCode(6, 20), "uri": deletionEventHTTPS, "has_password": deletionEventBool}
	case "conversation.access-update":
		return deletionEventAccess(data)
	case "conversation.history-update":
		return deletionEventHistory(data)
	case "conversation.create", "conversation.create-meeting":
		return deletionEventConversation(data, kind == "conversation.create-meeting")
	default:
		return false
	}
	return deletionEventFields(data, required, nullable, nil)
}

func deletionEventCode(minimum, maximum int) deletionEventValue {
	return func(raw json.RawMessage) bool {
		var value string
		if !deletionEventText(raw) || json.Unmarshal(raw, &value) != nil || len(value) < minimum || len(value) > maximum {
			return false
		}
		for _, ch := range value {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '=') {
				return false
			}
		}
		return true
	}
}

func deletionEventHTTPS(raw json.RawMessage) bool {
	var value string
	if !deletionEventText(raw) || json.Unmarshal(raw, &value) != nil || !strings.HasPrefix(value, "https:") {
		return false
	}
	for _, ch := range value {
		if ch <= 32 || ch >= 127 {
			return false
		}
	}
	u, err := url.Parse(value)
	return err == nil && u.IsAbs()
}

func deletionEventAccess(raw json.RawMessage) bool {
	if !deletionEventFields(raw, map[string]deletionEventValue{"access": deletionEventArray(deletionEventEnum("private", "invite", "link", "code"))}, map[string]deletionEventValue{"access_role": deletionEventEnum("private", "team", "activated", "non_activated"), "access_role_v2": deletionEventArray(deletionEventEnum("team_member", "non_team_member", "guest", "service"))}, nil) {
		return false
	}
	value := deletionEventObject(raw)
	return len(value["access_role"]) > 0 && !bytes.Equal(value["access_role"], []byte("null")) || len(value["access_role_v2"]) > 0 && !bytes.Equal(value["access_role_v2"], []byte("null"))
}

func deletionEventHistory(raw json.RawMessage) bool {
	if bytes.Equal(raw, []byte("null")) {
		return true
	}
	return deletionEventFields(raw, map[string]deletionEventValue{"depth": func(raw json.RawMessage) bool {
		var text string
		if !deletionEventText(raw) || json.Unmarshal(raw, &text) != nil {
			return false
		}
		// Native Attoparsec parseOnly intentionally does not require endOfInput.
		return strings.HasPrefix(text, "infinite") || len(text) > 0 && text[0] >= '0' && text[0] <= '9'
	}}, nil, nil)
}

func deletionEventNumberEnum(values ...int64) deletionEventValue {
	return func(raw json.RawMessage) bool {
		text, valid := deletionEventIntegerText(raw)
		if !valid {
			return false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return false
		}
		for _, value := range values {
			if n == value {
				return true
			}
		}
		return false
	}
}

func deletionEventNullable(valid deletionEventValue) deletionEventValue {
	return func(raw json.RawMessage) bool { return bytes.Equal(raw, []byte("null")) || valid(raw) }
}

func deletionEventMember(raw json.RawMessage, self bool) bool {
	nullable := map[string]deletionEventValue{"service": func(raw json.RawMessage) bool {
		return deletionEventFields(raw, map[string]deletionEventValue{"id": deletionEventUUID, "provider": deletionEventUUID}, nil, nil)
	}}
	if self {
		for key, value := range map[string]deletionEventValue{"otr_muted_status": deletionEventInteger(32, true), "otr_muted_ref": deletionEventText, "otr_archived": deletionEventBool, "otr_archived_ref": deletionEventText, "hidden_ref": deletionEventText, "conversation_role": deletionEventRole} {
			nullable[key] = value
		}
	}
	// OtherMember role and self hidden have native Alternative defaults, which
	// also handle invalid present values. Deprecated fields are similarly ignored.
	return deletionEventFields(raw, map[string]deletionEventValue{"qualified_id": deletionEventQualified}, nullable, nil)
}

func deletionEventConversation(raw json.RawMessage, meeting bool) bool {
	if !deletionEventAccess(raw) {
		return false
	}
	groupType := deletionEventEnum("group_conversation", "channel")
	if meeting {
		groupType = deletionEventEnum("group_conversation", "channel", "meeting")
	}
	if !deletionEventFields(raw, map[string]deletionEventValue{
		"qualified_id": deletionEventQualified, "type": deletionEventNumberEnum(0, 1, 2, 3),
		"members": func(raw json.RawMessage) bool {
			return deletionEventFields(raw, map[string]deletionEventValue{"self": func(raw json.RawMessage) bool { return deletionEventMember(raw, true) }, "others": deletionEventArray(func(raw json.RawMessage) bool { return deletionEventMember(raw, false) })}, nil, nil)
		},
	}, map[string]deletionEventValue{"creator": deletionEventUUID, "name": deletionEventText, "team": deletionEventUUID, "message_timer": deletionEventInteger(64, false), "receipt_mode": deletionEventInteger(32, true), "group_conv_type": groupType, "add_permission": deletionEventEnum("admins", "everyone"), "cells_state": deletionEventEnum("disabled", "pending", "ready"), "parent": deletionEventUUID, "history": deletionEventHistory, "protocol": deletionEventEnum("proteus", "mls", "mixed")}, nil) {
		return false
	}
	value := deletionEventObject(raw)
	var protocol string
	_ = json.Unmarshal(value["protocol"], &protocol)
	if protocol == "" || protocol == "proteus" {
		return true
	}
	return deletionEventFields(raw, map[string]deletionEventValue{"group_id": deletionEventBase64, "epoch": deletionEventInteger(64, false), "epoch_timestamp": deletionEventNullable(deletionEventTime), "cipher_suite": deletionEventNumberEnum(1, 2, 5, 7)}, nil, nil)
}
