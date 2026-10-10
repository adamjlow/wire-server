// SPDX-License-Identifier: AGPL-3.0-only
package backgroundworker

import (
	"encoding/json"
	"errors"
)

type queueDiscoveryPage struct {
	Items       []struct{ Name, VHost string }
	Page, Pages int64
}

// Network.RabbitMqAdmin's generic Page Queue decoder requires items, page,
// page_count and both name/vhost fields. Int accepts integral Scientific values;
// source traversal does not require response page to equal requested page.
func decodeQueueDiscoveryPage(body []byte) (queueDiscoveryPage, error) {
	var result queueDiscoveryPage
	invalid := errors.New("invalid queue discovery response")
	if !validAdminlessAccountJSONText(body) {
		return result, invalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return result, invalid
	}
	var ok bool
	result.Page, ok = adminlessVersionInteger(object["page"])
	if !ok {
		return result, invalid
	}
	result.Pages, ok = adminlessVersionInteger(object["page_count"])
	if !ok {
		return result, invalid
	}
	var items []json.RawMessage
	if json.Unmarshal(object["items"], &items) != nil || items == nil {
		return result, invalid
	}
	for _, item := range items {
		var row map[string]json.RawMessage
		var name, vhost *string
		if json.Unmarshal(item, &row) != nil || row == nil || json.Unmarshal(row["name"], &name) != nil || json.Unmarshal(row["vhost"], &vhost) != nil || name == nil || vhost == nil {
			return result, invalid
		}
		result.Items = append(result.Items, struct{ Name, VHost string }{*name, *vhost})
	}
	return result, nil
}
