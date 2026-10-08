package gundeck

import "testing"

func TestPayloadBoundaryCountsFilteredRowsAndAdmitsFirst(t *testing.T) {
	blob := []byte(`[{"seq":1}]`)
	rows := []notificationRow{{payload: &blob}, {payload: &blob}, {payload: &blob}}
	selected, remaining, truncated := truncatePayloadRows(rows, 5)
	if len(selected) != 1 || remaining != -6 || !truncated {
		t.Fatalf("selected=%d remaining=%d truncated=%v", len(selected), remaining, truncated)
	}
	// Haskell's last excluded row has no rest, so does not set its truncation flag.
	selected, remaining, truncated = truncatePayloadRows(rows[:2], 5)
	if len(selected) != 1 || remaining != -6 || truncated {
		t.Fatal("changed original last-row boundary")
	}
}
func TestStoredPayloadMustBeNonemptyObjectArray(t *testing.T) {
	for _, value := range []string{`null`, `[]`, `{}`, `[null]`, `[1]`, `bad`, `[{"seq":1}] trailing`} {
		if validPayload([]byte(value)) {
			t.Fatalf("accepted %q", value)
		}
	}
	if !validPayload([]byte(`[{"seq":9007199254740993}]`)) {
		t.Fatal("valid event rejected")
	}
}
