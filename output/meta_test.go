package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestStableJSONMetaEnvelopeIsDeterministic(t *testing.T) {
	page, perPage, totalPages, totalItems := 1, 2, 3, 0
	hasMore := false
	limit, remaining, reset := int64(100), int64(0), int64(1710000000)
	replayed := false
	envelope := Envelope{
		Data: []Node{
			{Label: "id", Value: "alpha"},
			{Label: "human note", Prose: true},
			{Label: "nested", Children: []Node{
				{Label: "kept", Value: "yes"},
				{Label: "nested note", Prose: true},
			}},
		},
		Meta: Meta{
			RequestID:  "req-1",
			RequestIDs: []string{"req-1", "req-0"},
			Pagination: &Pagination{
				Page:       &page,
				PerPage:    &perPage,
				TotalPages: &totalPages,
				TotalItems: &totalItems,
				NextCursor: "next",
				PrevCursor: "prev",
				HasMore:    &hasMore,
			},
			RateLimit: &RateLimit{
				Limit:     &limit,
				Remaining: &remaining,
				Reset:     &reset,
				Raw:       map[string]string{"X-RateLimit-Remaining": "0"},
			},
			Retry:       &Retry{Attempts: 2, WaitsMS: []int64{250, 500}},
			Idempotency: &Idempotency{Key: "idem-1", Replayed: &replayed},
			Extra:       map[string]any{"region": "us-1"},
		},
	}

	first, err := StableJSON(envelope)
	if err != nil {
		t.Fatalf("StableJSON first: %v", err)
	}
	second, err := StableJSON(envelope)
	if err != nil {
		t.Fatalf("StableJSON second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("metadata envelope output not repeatable:\n%s\n---\n%s", first, second)
	}
	out := string(first)
	for _, prose := range []string{"human note", "nested note"} {
		if strings.Contains(out, prose) {
			t.Fatalf("metadata envelope retained prose node %q:\n%s", prose, out)
		}
	}
	for _, want := range []string{`"request_id": "req-1"`, `"total_items": 0`, `"has_more": false`, `"remaining": 0`, `"replayed": false`} {
		if !strings.Contains(out, want) {
			t.Fatalf("metadata envelope missing %s:\n%s", want, out)
		}
	}
}

func TestStableJSONEmptyMetaIsEmptyObject(t *testing.T) {
	b, err := StableJSON(Envelope{
		Data: map[string]string{"id": "alpha"},
		Meta: Meta{},
	})
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	out := string(b)
	if !strings.Contains(out, `"meta": {}`) {
		t.Fatalf("empty metadata did not encode as empty object:\n%s", out)
	}
	if strings.Contains(out, "null") {
		t.Fatalf("empty metadata envelope contains null:\n%s", out)
	}
}
