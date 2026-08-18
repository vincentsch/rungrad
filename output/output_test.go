package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// selfRef builds a self-referential pointer cycle.
type selfRef struct {
	Label string
	Next  *selfRef
}

// selfMarshaler is a pointer-receiver json.Marshaler holding a self-pointer,
// proving custom-marshaler leaves are never keyed or recursed into.
type selfMarshaler struct {
	self *selfMarshaler
	data string
}

func (s *selfMarshaler) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.data)
}

func TestStableJSONIsDeterministicAndNewlineTerminated(t *testing.T) {
	v := map[string]any{"b": 2, "a": 1, "c": map[string]any{"z": 26, "y": 25}}
	first, err := StableJSON(v)
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	second, err := StableJSON(v)
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("StableJSON not byte-identical across calls")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatalf("StableJSON output is not newline terminated: %q", first)
	}
	// Keys must be alphabetically ordered for maps.
	if idxA, idxB := bytes.Index(first, []byte(`"a"`)), bytes.Index(first, []byte(`"b"`)); idxA > idxB {
		t.Fatalf("map keys not sorted: %s", first)
	}
	if !json.Valid(first) {
		t.Fatalf("StableJSON produced invalid JSON: %s", first)
	}
}

func TestDryRunPreviewMasksSecrets(t *testing.T) {
	p := DryRunPreview{
		Method: "POST",
		Path:   "/v1/tokens",
		Body: []Field{
			{Name: "name", Value: "ci-key"},
			{Name: "secret", Value: "super-secret-value", Secret: true},
		},
	}
	js, err := p.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if bytes.Contains(js, []byte("super-secret-value")) {
		t.Fatalf("secret leaked into JSON preview: %s", js)
	}
	if !bytes.Contains(js, []byte(maskedValue)) {
		t.Fatalf("expected masked value in JSON preview: %s", js)
	}

	var buf bytes.Buffer
	p.Render(&buf)
	if strings.Contains(buf.String(), "super-secret-value") {
		t.Fatalf("secret leaked into human preview: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "no changes were made") {
		t.Fatalf("human preview should state no changes were made: %s", buf.String())
	}
}

func TestDryRunPreviewRenderModeDisabledMatchesPlain(t *testing.T) {
	p := DryRunPreview{
		Method: "POST",
		Path:   "/v1/tokens",
		Query:  []Field{{Name: "force", Value: "true"}},
		Body: []Field{
			{Name: "name", Value: "ci-key"},
			{Name: "secret", Value: "super-secret-value", Secret: true},
		},
		Idempotency: "idem-123",
	}

	var plain, mode bytes.Buffer
	p.Render(&plain)
	p.RenderMode(&mode, TerminalMode{})
	if !bytes.Equal(mode.Bytes(), plain.Bytes()) {
		t.Fatalf("disabled mode differs from plain:\nmode=%q\nplain=%q", mode.String(), plain.String())
	}
	if bytes.Contains(mode.Bytes(), []byte("\x1b")) {
		t.Fatalf("disabled mode emitted ANSI: %q", mode.String())
	}
}

func TestDryRunPreviewRenderModeColorsLabelOnly(t *testing.T) {
	p := DryRunPreview{
		Method: "POST",
		Path:   "/items",
		Body:   []Field{{Name: "name", Value: "gamma"}},
	}

	var buf bytes.Buffer
	p.RenderMode(&buf, TerminalMode{ANSI: true, Color: true})
	wantFirst := ansiDryRun + "DRY RUN" + ansiReset + ": would POST /items\n"
	if !strings.HasPrefix(buf.String(), wantFirst) {
		t.Fatalf("first line mismatch:\ngot  %q\nwant %q", buf.String(), wantFirst)
	}
	if rest := strings.TrimPrefix(buf.String(), wantFirst); strings.Contains(rest, "\x1b") {
		t.Fatalf("ANSI escaped beyond DRY RUN label: %q", rest)
	}
}

func TestRenderTableEmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	RenderTable(&buf, Table{Columns: []string{"ID", "Name"}, Empty: "No projects."})
	if !strings.Contains(buf.String(), "No projects.") {
		t.Fatalf("expected empty message, got %q", buf.String())
	}
}

func TestRenderTableAligns(t *testing.T) {
	var buf bytes.Buffer
	RenderTable(&buf, Table{
		Columns: []string{"ID", "Name"},
		Rows:    [][]string{{"1", "alpha"}, {"2", "beta"}},
	})
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "alpha") {
		t.Fatalf("table missing content: %q", out)
	}
}

func TestRenderMutation(t *testing.T) {
	var buf bytes.Buffer
	RenderMutation(&buf, MutationSummary{
		Action:   "Created",
		Resource: "project",
		Name:     "Launch Plan",
		ID:       "123",
		Fields:   map[string]string{"status": "active"},
	})
	out := buf.String()
	if !strings.Contains(out, "Created project") || !strings.Contains(out, "123") {
		t.Fatalf("unexpected mutation render: %q", out)
	}
}

func TestRenderMutationModeDisabledMatchesPlain(t *testing.T) {
	m := MutationSummary{
		Action:   "Created",
		Resource: "project",
		Name:     "Launch Plan",
		ID:       "123",
		Fields:   map[string]string{"status": "active", "owner": "ci"},
		Notes:    []string{"review pending"},
	}

	var plain, mode bytes.Buffer
	RenderMutation(&plain, m)
	RenderMutationMode(&mode, m, TerminalMode{})
	if !bytes.Equal(mode.Bytes(), plain.Bytes()) {
		t.Fatalf("disabled mode differs from plain:\nmode=%q\nplain=%q", mode.String(), plain.String())
	}
	if bytes.Contains(mode.Bytes(), []byte("\x1b")) {
		t.Fatalf("disabled mode emitted ANSI: %q", mode.String())
	}
}

func TestRenderMutationModeColorsActionOnly(t *testing.T) {
	var buf bytes.Buffer
	RenderMutationMode(&buf, MutationSummary{
		Action:   "Created",
		Resource: "item",
		Name:     "gamma",
	}, TerminalMode{ANSI: true, Color: true})
	want := ansiMutationAction + "Created" + ansiReset + " item gamma\n"
	if buf.String() != want {
		t.Fatalf("styled mutation mismatch:\ngot  %q\nwant %q", buf.String(), want)
	}
}

func TestRenderModesPartialAreUnstyled(t *testing.T) {
	preview := DryRunPreview{
		Method: "POST",
		Path:   "/items",
		Body:   []Field{{Name: "name", Value: "gamma"}},
	}
	mutation := MutationSummary{Action: "Created", Resource: "item", Name: "gamma"}
	modes := []TerminalMode{
		{ANSI: true},
		{Color: true},
	}

	var plainPreview bytes.Buffer
	preview.Render(&plainPreview)
	var plainMutation bytes.Buffer
	RenderMutation(&plainMutation, mutation)

	for _, mode := range modes {
		t.Run(fmt.Sprintf("ansi=%t_color=%t", mode.ANSI, mode.Color), func(t *testing.T) {
			var gotPreview bytes.Buffer
			preview.RenderMode(&gotPreview, mode)
			if !bytes.Equal(gotPreview.Bytes(), plainPreview.Bytes()) {
				t.Fatalf("partial mode preview differs from plain:\ngot  %q\nwant %q", gotPreview.String(), plainPreview.String())
			}
			if bytes.Contains(gotPreview.Bytes(), []byte("\x1b")) {
				t.Fatalf("partial mode preview emitted ANSI: %q", gotPreview.String())
			}

			var gotMutation bytes.Buffer
			RenderMutationMode(&gotMutation, mutation, mode)
			if !bytes.Equal(gotMutation.Bytes(), plainMutation.Bytes()) {
				t.Fatalf("partial mode mutation differs from plain:\ngot  %q\nwant %q", gotMutation.String(), plainMutation.String())
			}
			if bytes.Contains(gotMutation.Bytes(), []byte("\x1b")) {
				t.Fatalf("partial mode mutation emitted ANSI: %q", gotMutation.String())
			}
		})
	}
}

func TestSanitizeControlBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"csi", "Bad\x1b[31mName", "Bad Name"},
		{"osc bel", "a\x1b]0;title\x07b", "a b"},
		{"osc st", "a\x1b]8;;https://example.test\x1b\\b", "a b"},
		{"unknown escape", "a\x1b(Bb", "a b"},
		{"plain control", "a\bb", "a b"},
		{"tabs and newlines", "a\tb\nc", "a\tb\nc"},
		{"crlf", "a\r\nb", "a\nb"},
		{"utf8", "cost €\x1b[31m ok", "cost € ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(SanitizeControlBytes([]byte(tt.in))); got != tt.want {
				t.Fatalf("SanitizeControlBytes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderHumanSanitizesWhenRequested(t *testing.T) {
	got := RenderHuman(TerminalMode{Sanitize: true}, func(w io.Writer) {
		fmt.Fprint(w, "Bad\x1b[31mName\n")
	})
	if string(got) != "Bad Name\n" {
		t.Fatalf("RenderHuman sanitized = %q", got)
	}
	if got := RenderHuman(TerminalMode{}, nil); got != nil {
		t.Fatalf("nil renderer produced %q, want nil", got)
	}
}

func TestRenderHumanWithTransformRunsBeforeSanitize(t *testing.T) {
	sawRawControl := false
	got := RenderHumanWith(TerminalMode{Sanitize: true}, func(w io.Writer) {
		fmt.Fprint(w, "secret\bvalue\n")
	}, func(b []byte) []byte {
		sawRawControl = bytes.Contains(b, []byte("\b"))
		return bytes.ReplaceAll(b, []byte("secret\bvalue"), []byte("safe\bvalue"))
	})
	if !sawRawControl {
		t.Fatalf("transform did not observe the raw control byte")
	}
	if string(got) != "safe value\n" {
		t.Fatalf("RenderHumanWith transformed/sanitized = %q", got)
	}
}

func TestRenderMutationModeEmptyActionEscapeFree(t *testing.T) {
	var buf bytes.Buffer
	RenderMutationMode(&buf, MutationSummary{Resource: "item"}, TerminalMode{ANSI: true, Color: true})
	if bytes.Contains(buf.Bytes(), []byte("\x1b")) {
		t.Fatalf("empty action emitted ANSI: %q", buf.String())
	}
}

func TestStableJSONOmitsProseNodes(t *testing.T) {
	js, err := StableJSON([]Node{
		{Label: "id", Value: "1"},
		{Label: "human-only hint", Prose: true},
		{Label: "parent", Children: []Node{
			{Label: "nested hint", Prose: true},
			{Label: "name", Value: "alpha"},
		}},
	})
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if bytes.Contains(js, []byte("human-only hint")) || bytes.Contains(js, []byte("nested hint")) {
		t.Fatalf("prose node leaked into JSON: %s", js)
	}
	if !bytes.Contains(js, []byte("alpha")) {
		t.Fatalf("non-prose node missing from JSON: %s", js)
	}
}

func TestStableJSONOmitsNestedProseNodes(t *testing.T) {
	type payload struct {
		At    time.Time      `json:"at"`
		Nodes []Node         `json:"nodes"`
		Meta  map[string]any `json:"meta"`
	}
	at := time.Date(2026, 6, 13, 10, 30, 0, 0, time.UTC)
	js, err := StableJSON(payload{
		At: at,
		Nodes: []Node{
			{Label: "top hint", Prose: true},
			{Label: "id", Value: "1"},
		},
		Meta: map[string]any{
			"children": []Node{
				{Label: "nested hint", Prose: true},
				{Label: "name", Value: "alpha"},
			},
			"pointer": &Node{Label: "pointer hint", Prose: true},
			"array":   [2]Node{{Label: "array hint", Prose: true}, {Label: "status", Value: "ok"}},
		},
	})
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	for _, leaked := range []string{"top hint", "nested hint", "pointer hint", "array hint"} {
		if bytes.Contains(js, []byte(leaked)) {
			t.Fatalf("prose node %q leaked into JSON: %s", leaked, js)
		}
	}
	for _, kept := range []string{"alpha", "status", "ok"} {
		if !bytes.Contains(js, []byte(kept)) {
			t.Fatalf("non-prose value %q missing from JSON: %s", kept, js)
		}
	}
	if !bytes.Contains(js, []byte("2026-06-13T10:30:00Z")) {
		t.Fatalf("custom JSON marshaler value was not preserved: %s", js)
	}
}

func TestStableJSONDetectsCycles(t *testing.T) {
	cases := []struct {
		name  string
		build func() any
	}{
		{"self pointer", func() any {
			p := &selfRef{Label: "x"}
			p.Next = p
			return p
		}},
		{"map through any", func() any {
			m := map[string]any{"k": "v"}
			m["self"] = m
			return m
		}},
		{"slice through any", func() any {
			s := make([]any, 1)
			s[0] = s
			return s
		}},
		{"node children self", func() any {
			s := []Node{{Label: "x"}}
			s[0].Children = s
			return s
		}},
		{"node value cycle", func() any {
			s := make([]any, 1)
			s[0] = Node{Label: "loop", Value: s}
			return s
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := StableJSON(c.build())
			if err == nil {
				t.Fatalf("expected a cycle error, got bytes: %s", b)
			}
			if b != nil {
				t.Fatalf("expected nil bytes on cycle, got: %s", b)
			}
			if !strings.Contains(err.Error(), "cycle detected") {
				t.Fatalf("error %q missing %q", err.Error(), "cycle detected")
			}
		})
	}
}

func TestStableJSONCycleErrorIsDeterministic(t *testing.T) {
	build := func() any {
		m := map[string]any{"k": "v"}
		m["self"] = m
		return m
	}
	_, err1 := StableJSON(build())
	_, err2 := StableJSON(build())
	if err1 == nil || err2 == nil {
		t.Fatalf("expected cycle errors, got %v / %v", err1, err2)
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("cycle error not deterministic: %q vs %q", err1.Error(), err2.Error())
	}
	if !strings.Contains(err1.Error(), "cycle detected") {
		t.Fatalf("error %q missing %q", err1.Error(), "cycle detected")
	}
}

func TestStableJSONSerializesSharedDAG(t *testing.T) {
	t.Run("shared pointer", func(t *testing.T) {
		type leaf struct {
			V string `json:"v"`
		}
		type diamond struct {
			A *leaf `json:"a"`
			B *leaf `json:"b"`
		}
		shared := &leaf{V: "x"}
		b, err := StableJSON(diamond{A: shared, B: shared})
		if err != nil {
			t.Fatalf("StableJSON: %v", err)
		}
		if bytes.Count(b, []byte(`"x"`)) != 2 {
			t.Fatalf("shared pointer not serialized twice: %s", b)
		}
	})
	t.Run("shared slice", func(t *testing.T) {
		type diamond struct {
			A []string `json:"a"`
			B []string `json:"b"`
		}
		shared := []string{"x"}
		b, err := StableJSON(diamond{A: shared, B: shared})
		if err != nil {
			t.Fatalf("StableJSON: %v", err)
		}
		if bytes.Count(b, []byte(`"x"`)) != 2 {
			t.Fatalf("shared slice not serialized twice: %s", b)
		}
	})
	t.Run("shared map", func(t *testing.T) {
		type diamond struct {
			A map[string]string `json:"a"`
			B map[string]string `json:"b"`
		}
		shared := map[string]string{"k": "x"}
		b, err := StableJSON(diamond{A: shared, B: shared})
		if err != nil {
			t.Fatalf("StableJSON: %v", err)
		}
		if bytes.Count(b, []byte(`"x"`)) != 2 {
			t.Fatalf("shared map not serialized twice: %s", b)
		}
	})
}

func TestStableJSONSharedNodeChildrenSerialize(t *testing.T) {
	shared := []Node{{Label: "leaf", Value: "v"}}
	top := []Node{
		{Label: "a", Children: shared},
		{Label: "b", Children: shared},
	}
	b, err := StableJSON(top)
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if bytes.Count(b, []byte(`"leaf"`)) != 2 {
		t.Fatalf("shared node children not serialized for both siblings: %s", b)
	}
}

func TestStableJSONCustomMarshalerSelfReferenceIsLeaf(t *testing.T) {
	m := &selfMarshaler{data: "ok"}
	m.self = m
	b, err := StableJSON(m)
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if !bytes.Contains(b, []byte("ok")) {
		t.Fatalf("custom marshaler value missing: %s", b)
	}
}

func TestStableJSONNodeValueOmitsProse(t *testing.T) {
	n := Node{Label: "x", Value: []Node{
		{Label: "value hint", Prose: true},
		{Label: "kept", Value: "yes"},
	}}
	b, err := StableJSON(n)
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if bytes.Contains(b, []byte("value hint")) {
		t.Fatalf("prose node in Node.Value leaked: %s", b)
	}
	if !bytes.Contains(b, []byte("kept")) || !bytes.Contains(b, []byte("yes")) {
		t.Fatalf("non-prose Node.Value content missing: %s", b)
	}
}

func TestStableJSONAcyclicProseUnchanged(t *testing.T) {
	b, err := StableJSON([]Node{
		{Label: "id", Value: "1"},
		{Label: "human-only hint", Prose: true},
		{Label: "parent", Children: []Node{
			{Label: "nested hint", Prose: true},
			{Label: "name", Value: "alpha"},
		}},
	})
	if err != nil {
		t.Fatalf("StableJSON: %v", err)
	}
	if bytes.Contains(b, []byte("hint")) {
		t.Fatalf("prose leaked: %s", b)
	}
	if !bytes.Contains(b, []byte("alpha")) || !bytes.Contains(b, []byte(`"1"`)) {
		t.Fatalf("kept values missing: %s", b)
	}
}

func TestStableJSONConcurrentCallsArePerCall(t *testing.T) {
	const n = 64
	type result struct {
		cyclic bool
		out    []byte
		err    error
		want   []byte
	}
	results := make([]result, 2*n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			s := make([]any, 1)
			s[0] = s
			out, err := StableJSON(s)
			results[i] = result{cyclic: true, out: out, err: err}
		}(i)
		go func(i int) {
			defer wg.Done()
			out, err := StableJSON(map[string]any{"i": i, "ok": true})
			want := []byte(fmt.Sprintf("{\n  \"i\": %d,\n  \"ok\": true\n}\n", i))
			results[n+i] = result{cyclic: false, out: out, err: err, want: want}
		}(i)
	}
	wg.Wait()
	for idx, r := range results {
		if r.cyclic {
			if r.err == nil || !strings.Contains(r.err.Error(), "cycle detected") {
				t.Errorf("result %d (cyclic): err = %v, want cycle detected", idx, r.err)
			}
			if r.out != nil {
				t.Errorf("result %d (cyclic): out = %q, want nil", idx, r.out)
			}
		} else {
			if r.err != nil {
				t.Errorf("result %d (acyclic): unexpected err %v", idx, r.err)
			}
			if !bytes.Equal(r.out, r.want) {
				t.Errorf("result %d (acyclic): out = %q, want %q", idx, r.out, r.want)
			}
		}
	}
}
