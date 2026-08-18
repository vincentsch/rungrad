package testutil

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMockServer(t *testing.T) {
	srv := MockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get mock server: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAssertStableJSON(t *testing.T) {
	var body map[string]bool
	AssertStableJSON(t, "{\n  \"ok\": true\n}\n", &body)
	if !body["ok"] {
		encoded, _ := json.Marshal(body)
		t.Fatalf("unexpected body: %s", encoded)
	}
}
