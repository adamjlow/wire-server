package cannon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type notification struct {
	ID        string           `json:"id"`
	Transient bool             `json:"transient"`
	Payload   []map[string]any `json:"payload"`
}
type pushTarget struct {
	User       string `json:"user_id"`
	Connection string `json:"conn_id"`
}
type bulkItem struct {
	Notification json.RawMessage `json:"notification"`
	Targets      json.RawMessage `json:"targets"`
}
type parsedBulkItem struct {
	notification notification
	targets      []pushTarget
}

type pushResult struct {
	ID     string     `json:"notif_id"`
	Target pushTarget `json:"target"`
	Status string     `json:"status"`
}

func (s *Server) bulk(w http.ResponseWriter, r *http.Request) {
	if !jsonType(r) {
		plain(w, r, 415, "")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, r, 500, "server-error", "Server Error")
		return
	}
	if strings.TrimSpace(string(raw)) == "{" {
		plain(w, r, 400, "Unexpected end-of-input, expecting record key literal or }")
		return
	}
	var request struct {
		Items json.RawMessage `json:"bulkpush_req"`
	}
	if json.Unmarshal(raw, &request) != nil || len(request.Items) == 0 || string(request.Items) == "null" {
		plain(w, r, 400, "Error in $: key \"bulkpush_req\" not found")
		return
	}
	var items []bulkItem
	if json.Unmarshal(request.Items, &items) != nil || items == nil {
		plain(w, r, 400, "Error in $.bulkpush_req: parsing [] failed")
		return
	}
	parsed := make([]parsedBulkItem, 0, len(items))
	for _, item := range items {
		var n notification
		var fields map[string]json.RawMessage
		if json.Unmarshal(item.Notification, &fields) != nil || fields == nil || len(fields["id"]) == 0 || len(fields["payload"]) == 0 || decodeNumbers(item.Notification, &n) != nil || !isUUID(n.ID) || len(n.Payload) == 0 {
			plain(w, r, 400, "Error in $.bulkpush_req: invalid notification")
			return
		}
		for _, payload := range n.Payload {
			if payload == nil {
				plain(w, r, 400, "Error in $.bulkpush_req: invalid notification payload")
				return
			}
		}
		if v, ok := fields["transient"]; ok && string(v) == "null" {
			plain(w, r, 400, "Error in $.bulkpush_req: invalid transient")
			return
		}
		var targets []pushTarget
		var targetFields []map[string]json.RawMessage
		if len(item.Targets) == 0 || json.Unmarshal(item.Targets, &targets) != nil || targets == nil || json.Unmarshal(item.Targets, &targetFields) != nil {
			plain(w, r, 400, "Error in $.bulkpush_req: invalid targets")
			return
		}
		for i, target := range targets {
			if !isUUID(target.User) || len(targetFields[i]["conn_id"]) == 0 || string(targetFields[i]["conn_id"]) == "null" {
				plain(w, r, 400, "Error in $.bulkpush_req: invalid target")
				return
			}
			targets[i].User = strings.ToLower(target.User)
		}
		n.ID = strings.ToLower(n.ID)
		parsed = append(parsed, parsedBulkItem{n, targets})
	}
	results := make([]pushResult, 0)
	for _, item := range parsed {
		payload, _ := json.Marshal(item.notification)
		itemResults := make([]pushResult, len(item.targets))
		jobs := make(chan int)
		var workers sync.WaitGroup
		count := min(16, len(item.targets))
		for range count {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for i := range jobs {
					target := item.targets[i]
					status := "push_status_gone"
					if s.push(socketKey{target.User, target.Connection}, payload) {
						status = "push_status_ok"
					}
					itemResults[i] = pushResult{item.notification.ID, target, status}
				}
			}()
		}
		for i := range item.targets {
			jobs <- i
		}
		close(jobs)
		workers.Wait()
		results = append(results, itemResults...)
	}
	jsonResponse(w, r, 200, map[string]any{"bulkpush_resp": results})
}

func decodeNumbers(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}
