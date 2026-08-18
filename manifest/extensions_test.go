package manifest

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

const extensionNS = "example.com/product"

type customJSONExtensionValue string

func (v customJSONExtensionValue) MarshalJSON() ([]byte, error) {
	return []byte(`"custom"`), nil
}

type customTextExtensionValue string

func (v customTextExtensionValue) MarshalText() ([]byte, error) {
	return []byte(v), nil
}

func sharedPrefixExtensionSlice() []any {
	s := make([]any, 2)
	s[0] = "x"
	s[1] = s[:1]
	return s
}

func TestValidateExtensionSet(t *testing.T) {
	cycleMap := map[string]any{}
	cycleMap["self"] = cycleMap
	cycleSlice := []any{nil}
	cycleSlice[0] = cycleSlice

	tests := []struct {
		name string
		set  ExtensionSet
		want string
	}{
		{
			name: "valid unknown namespace",
			set: ExtensionSet{extensionNS: {
				"owner": "platform",
				"count": 2,
				"tags":  []string{"stable", "listed"},
			}},
		},
		{name: "uppercase namespace", set: ExtensionSet{"Example.com/product": {}}, want: "invalid namespace"},
		{name: "reserved slash namespace", set: ExtensionSet{"rungrad/x": {}}, want: "reserved namespace"},
		{name: "reserved dot namespace", set: ExtensionSet{"rungrad.x/product": {}}, want: "reserved namespace"},
		{name: "multi slash namespace", set: ExtensionSet{"github.com/vincentsch/rungrad/product": {}}, want: "invalid namespace"},
		{name: "nil namespace object", set: ExtensionSet{extensionNS: nil}, want: "null value"},
		{name: "null field value", set: ExtensionSet{extensionNS: {"owner": nil}}, want: "null value"},
		{name: "non finite float", set: ExtensionSet{extensionNS: {"ratio": math.Inf(1)}}, want: "non-finite number"},
		{name: "json number NaN", set: ExtensionSet{extensionNS: {"ratio": json.Number("NaN")}}, want: "invalid number"},
		{name: "json number Infinity", set: ExtensionSet{extensionNS: {"ratio": json.Number("Infinity")}}, want: "invalid number"},
		{name: "json number leading zero", set: ExtensionSet{extensionNS: {"ratio": json.Number("01")}}, want: "invalid number"},
		{name: "func value", set: ExtensionSet{extensionNS: {"fn": func() {}}}, want: "unsupported value type func"},
		{name: "chan value", set: ExtensionSet{extensionNS: {"ch": make(chan int)}}, want: "unsupported value type chan"},
		{name: "raw message value", set: ExtensionSet{extensionNS: {"raw": json.RawMessage(`null`)}}, want: "custom JSON marshaler"},
		{name: "custom json marshaler", set: ExtensionSet{extensionNS: {"raw": customJSONExtensionValue("x")}}, want: "custom JSON marshaler"},
		{name: "custom text marshaler", set: ExtensionSet{extensionNS: {"text": customTextExtensionValue("x")}}, want: "custom text marshaler"},
		{name: "non string map key", set: ExtensionSet{extensionNS: {"labels": map[int]string{1: "one"}}}, want: "non-string map key"},
		{name: "reference map cycle", set: ExtensionSet{extensionNS: {"cycle": cycleMap}}, want: "cycle detected"},
		{name: "reference slice cycle", set: ExtensionSet{extensionNS: {"cycle": cycleSlice}}, want: "cycle detected"},
		{name: "core field supports dry run", set: ExtensionSet{extensionNS: {"supports_dry_run": true}}, want: "core field contradiction"},
		{name: "core field confirmation", set: ExtensionSet{extensionNS: {"requires_confirmation": true}}, want: "core field contradiction"},
		{name: "empty namespace object", set: ExtensionSet{extensionNS: {}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExtensionSet(tt.set)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ValidateExtensionSet() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateExtensionSet() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateExtensionSetAllowsAcyclicSharedPrefixSlice(t *testing.T) {
	set := ExtensionSet{extensionNS: {"tree": sharedPrefixExtensionSlice()}}
	if err := ValidateExtensionSet(set); err != nil {
		t.Fatalf("ValidateExtensionSet(shared prefix slice) = %v", err)
	}
	encoded, err := EncodeExtensions(set)
	if err != nil {
		t.Fatalf("EncodeExtensions(shared prefix slice) = %v", err)
	}
	want := `{"example.com/product":{"tree":["x",["x"]]}}`
	if encoded != want {
		t.Fatalf("EncodeExtensions(shared prefix slice) = %s, want %s", encoded, want)
	}
}

func TestEncodeExtensions(t *testing.T) {
	set := ExtensionSet{
		extensionNS: {
			"zeta": map[string]any{"b": 2, "a": 1},
			"alpha": []string{
				"first",
				"second",
			},
			"large": int64(9007199254740993),
		},
	}
	first, err := EncodeExtensions(set)
	if err != nil {
		t.Fatalf("EncodeExtensions() = %v", err)
	}
	second, err := EncodeExtensions(set)
	if err != nil {
		t.Fatalf("EncodeExtensions() repeat = %v", err)
	}
	if first != second {
		t.Fatalf("EncodeExtensions not deterministic:\n%s\n---\n%s", first, second)
	}
	want := `{"example.com/product":{"alpha":["first","second"],"large":9007199254740993,"zeta":{"a":1,"b":2}}}`
	if first != want {
		t.Fatalf("EncodeExtensions() = %s, want %s", first, want)
	}

	if got, err := EncodeExtensions(nil); err != nil || got != "" {
		t.Fatalf("EncodeExtensions(nil) = %q, %v; want empty nil", got, err)
	}
	if got, err := EncodeExtensions(ExtensionSet{}); err != nil || got != "" {
		t.Fatalf("EncodeExtensions(empty) = %q, %v; want empty nil", got, err)
	}
	if got, err := EncodeExtensions(ExtensionSet{extensionNS: {}}); err != nil || got != "" {
		t.Fatalf("EncodeExtensions(empty namespace) = %q, %v; want empty nil", got, err)
	}
	if _, err := EncodeExtensions(ExtensionSet{"Example.com/product": {}}); err == nil {
		t.Fatal("EncodeExtensions invalid empty namespace = nil, want error")
	}
	if _, err := EncodeExtensions(ExtensionSet{extensionNS: nil}); err == nil {
		t.Fatal("EncodeExtensions nil namespace object = nil, want error")
	}
	if _, err := EncodeExtensions(ExtensionSet{extensionNS: {"fn": func() {}}}); err == nil {
		t.Fatal("EncodeExtensions invalid set = nil, want error")
	}
}

func TestDecodeExtensions(t *testing.T) {
	encoded, err := EncodeExtensions(ExtensionSet{extensionNS: {
		"owner": "platform",
		"large": json.Number("9007199254740993"),
		"tags":  []any{"stable", "listed"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeExtensions(encoded)
	if err != nil {
		t.Fatalf("DecodeExtensions() = %v", err)
	}
	reencoded, err := EncodeExtensions(decoded)
	if err != nil {
		t.Fatalf("EncodeExtensions(decoded) = %v", err)
	}
	if reencoded != encoded {
		t.Fatalf("round trip = %s, want %s", reencoded, encoded)
	}
	if got, ok := decoded[extensionNS]["large"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("large value = %#v, want json.Number fidelity", decoded[extensionNS]["large"])
	}
	emptyArray, err := DecodeExtensions(`{"example.com/product":{"tags":[]}}`)
	if err != nil {
		t.Fatalf("DecodeExtensions(empty array) = %v", err)
	}
	tags, ok := emptyArray[extensionNS]["tags"].([]any)
	if !ok || tags == nil || len(tags) != 0 {
		t.Fatalf("empty array = %#v, want non-nil []any{}", emptyArray[extensionNS]["tags"])
	}
	if err := ValidateExtensionSet(emptyArray); err != nil {
		t.Fatalf("ValidateExtensionSet(decoded empty array) = %v", err)
	}
	if got, err := DecodeExtensions("   "); err != nil || got != nil {
		t.Fatalf("DecodeExtensions(blank) = %#v, %v; want nil nil", got, err)
	}

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "duplicate top key", raw: `{"example.com/product":{},"example.com/product":{}}`, want: "duplicate key"},
		{name: "duplicate nested key", raw: `{"example.com/product":{"owner":"a","owner":"b"}}`, want: "duplicate key"},
		{name: "trailing data", raw: `{"example.com/product":{}} {}`, want: "trailing data"},
		{name: "top array", raw: `[]`, want: "extensions must be a JSON object"},
		{name: "null namespace", raw: `{"example.com/product":null}`, want: "null value"},
		{name: "non object namespace", raw: `{"example.com/product":1}`, want: "non-object namespace value"},
		{name: "invalid namespace", raw: `{"Example.com/product":{}}`, want: "invalid namespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeExtensions(tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DecodeExtensions() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRequireExtensionFields(t *testing.T) {
	set := ExtensionSet{extensionNS: {
		"owner":  "platform",
		"zero":   0,
		"false":  false,
		"list":   []string{"a"},
		"object": map[string]string{"k": "v"},
	}}
	if err := RequireExtensionFields(set, extensionNS, "owner", "zero", "false", "list", "object"); err != nil {
		t.Fatalf("RequireExtensionFields(valid) = %v", err)
	}

	for _, tt := range []struct {
		name  string
		set   ExtensionSet
		field string
		want  string
	}{
		{name: "missing namespace", set: nil, field: "owner", want: "required namespace is missing"},
		{name: "missing field", set: set, field: "missing", want: "required field is empty or missing"},
		{name: "empty string", set: ExtensionSet{extensionNS: {"owner": ""}}, field: "owner", want: "required field is empty or missing"},
		{name: "empty array", set: ExtensionSet{extensionNS: {"items": []string{}}}, field: "items", want: "required field is empty or missing"},
		{name: "empty object", set: ExtensionSet{extensionNS: {"obj": map[string]any{}}}, field: "obj", want: "required field is empty or missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireExtensionFields(tt.set, extensionNS, tt.field)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RequireExtensionFields() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRequireExtensionEnum(t *testing.T) {
	set := ExtensionSet{extensionNS: {"status": "beta", "count": 2}}
	if err := RequireExtensionEnum(set, extensionNS, "status", "alpha", "beta"); err != nil {
		t.Fatalf("RequireExtensionEnum(valid) = %v", err)
	}
	if err := RequireExtensionEnum(set, extensionNS, "status", "stable"); err == nil || !strings.Contains(err.Error(), "not in allowed set") {
		t.Fatalf("RequireExtensionEnum(out of set) = %v", err)
	}
	if err := RequireExtensionEnum(set, extensionNS, "count", "2"); err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("RequireExtensionEnum(non-string) = %v", err)
	}
}

func TestRequireExtensionDocPath(t *testing.T) {
	valid := ExtensionSet{extensionNS: {"docs_path": "docs/commands/read.md"}}
	if err := RequireExtensionDocPath(valid, extensionNS, "docs_path"); err != nil {
		t.Fatalf("RequireExtensionDocPath(valid) = %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "absolute", path: "/docs/read.md", want: "relative path"},
		{name: "url", path: "https://example.com/read", want: "must not be a URL"},
		{name: "scheme", path: "mailto:docs@example.com", want: "must not be a URL"},
		{name: "escape", path: "../read.md", want: "must not escape via .."},
		{name: "unclean", path: "docs/./read.md", want: "clean path"},
		{name: "empty", path: "", want: "empty or missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireExtensionDocPath(ExtensionSet{extensionNS: {"docs_path": tt.path}}, extensionNS, "docs_path")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RequireExtensionDocPath() = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateManifestExtensions(t *testing.T) {
	valid := validManifest()
	valid.Commands[1].Extensions = ExtensionSet{extensionNS: {"owner": "platform"}}
	if err := Validate(&valid); err != nil {
		t.Fatalf("Validate(valid extensions) = %v", err)
	}

	invalid := validManifest()
	invalid.Commands[1].Extensions = ExtensionSet{extensionNS: {"requires_confirmation": true}}
	if err := Validate(&invalid); err == nil || !strings.Contains(err.Error(), "core field contradiction") {
		t.Fatalf("Validate(invalid extensions) = %v", err)
	}

	without := validManifest()
	b, err := json.Marshal(without)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "extensions") {
		t.Fatalf("manifest without extensions contains extensions key: %s", b)
	}
	if err := Validate(&without); err != nil {
		t.Fatalf("Validate(nil extensions) = %v", err)
	}
}

func TestManifestExtensionJSONDecodeRejectsDuplicatesAndPreservesNumbers(t *testing.T) {
	raw := `{
		"schema_version": "rungrad-manifest/1",
		"spec_version": "rungrad-spec/1",
		"tool_name": "rgdemo",
		"tool_version": "v0.0.0",
		"global_flags": [],
		"commands": [
			{"path": [], "use": "rgdemo", "examples": [], "related": [], "output_modes": [], "local_flags": []},
			{"path": ["read"], "use": "read", "examples": [], "related": [], "output_modes": [], "local_flags": [], "extensions": {"example.com/product": {"large": 9007199254740993, "tags": []}}}
		]
	}`
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal manifest with extensions: %v", err)
	}
	if err := Validate(&m); err != nil {
		t.Fatalf("Validate(decoded manifest) = %v", err)
	}
	read := m.Commands[1].Extensions[extensionNS]
	if got, ok := read["large"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("large extension = %#v, want json.Number fidelity", read["large"])
	}
	if tags, ok := read["tags"].([]any); !ok || tags == nil || len(tags) != 0 {
		t.Fatalf("tags extension = %#v, want non-nil empty []any", read["tags"])
	}

	duplicate := `{
		"schema_version": "rungrad-manifest/1",
		"spec_version": "rungrad-spec/1",
		"tool_name": "rgdemo",
		"global_flags": [],
		"commands": [
			{"path": [], "use": "rgdemo", "examples": [], "related": [], "output_modes": [], "local_flags": []},
			{"path": ["read"], "use": "read", "examples": [], "related": [], "output_modes": [], "local_flags": [], "extensions": {"example.com/product": {"owner": "a", "owner": "b"}}}
		]
	}`
	if err := json.Unmarshal([]byte(duplicate), &m); err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("unmarshal duplicate extension keys = %v, want duplicate key error", err)
	}
}

func TestDecodedExtensionEqualityUsesCanonicalEncode(t *testing.T) {
	left := ExtensionSet{extensionNS: {"large": int64(9007199254740993)}}
	encoded, err := EncodeExtensions(left)
	if err != nil {
		t.Fatal(err)
	}
	right, err := DecodeExtensions(encoded)
	if err != nil {
		t.Fatal(err)
	}
	rightEncoded, err := EncodeExtensions(right)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded, rightEncoded) {
		t.Fatalf("canonical forms differ: %q vs %q", encoded, rightEncoded)
	}
}
