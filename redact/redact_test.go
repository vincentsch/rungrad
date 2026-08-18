package redact

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRegistryIgnoresEmptyWhitespaceAndShortValues(t *testing.T) {
	r := NewRegistry()
	for _, value := range []string{"", "   ", "abcd"} {
		r.Add(value)
	}
	in := "empty  abcd value"
	if got := r.RedactString(in); got != in {
		t.Fatalf("RedactString with only ignored values = %q, want %q", got, in)
	}
}

func TestRedactBytesAndString(t *testing.T) {
	r := NewRegistry()
	r.Add("secret-token")
	r.Add("other-secret")
	r.Add("secret-token")

	got := string(r.RedactBytes([]byte("secret-token and other-secret")))
	want := Token + " and " + Token
	if got != want {
		t.Fatalf("RedactBytes = %q, want %q", got, want)
	}
	if got := r.RedactString(`escaped secret-token`); got != `escaped `+Token {
		t.Fatalf("RedactString = %q", got)
	}

	r.Add(`quote"secret`)
	if got := r.RedactString(`escaped quote\"secret`); got != `escaped `+Token {
		t.Fatalf("RedactString escaped form = %q", got)
	}
}

func TestRedactBytesOverlappingSecretsAreDeterministic(t *testing.T) {
	input := []byte("secret-token-extended and secret-token")
	first := NewRegistry()
	first.Add("secret-token")
	first.Add("secret-token-extended")
	second := NewRegistry()
	second.Add("secret-token-extended")
	second.Add("secret-token")

	gotFirst := first.RedactBytes(append([]byte(nil), input...))
	gotSecond := second.RedactBytes(append([]byte(nil), input...))
	if !bytes.Equal(gotFirst, gotSecond) {
		t.Fatalf("registration order changed output:\nfirst  %q\nsecond %q", gotFirst, gotSecond)
	}
	want := []byte(Token + " and " + Token)
	if !bytes.Equal(gotFirst, want) {
		t.Fatalf("overlap redaction = %q, want %q", gotFirst, want)
	}
}

func TestRedactBytesNoOpCases(t *testing.T) {
	var nilRegistry *Registry
	input := []byte("plain")
	if got := nilRegistry.RedactBytes(input); !bytes.Equal(got, input) {
		t.Fatalf("nil registry changed bytes: %q", got)
	}
	r := NewRegistry()
	if got := r.RedactBytes(nil); got != nil {
		t.Fatalf("empty input = %q, want nil", got)
	}
	if got := string(r.RedactBytes(input)); got != "plain" {
		t.Fatalf("empty registry changed bytes: %q", got)
	}
}

func TestRedactJSONStringValue(t *testing.T) {
	r := NewRegistry()
	r.Add("secret-token")
	out := r.RedactJSON([]byte(`{"token":"secret-token","ok":true}`))
	assertValidJSON(t, out)
	if bytes.Contains(out, []byte("secret-token")) || !bytes.Contains(out, []byte(Token)) {
		t.Fatalf("JSON secret not redacted: %s", out)
	}
}

func TestRedactJSONEmbeddedStringValue(t *testing.T) {
	r := NewRegistry()
	r.Add("secret-token")
	out := r.RedactJSON([]byte(`{"error":"backend echoed secret-token in message"}`))
	assertValidJSON(t, out)
	if bytes.Contains(out, []byte("secret-token")) || !bytes.Contains(out, []byte("echoed "+Token+" in")) {
		t.Fatalf("embedded JSON secret not redacted: %s", out)
	}
}

func TestRedactJSONEscapedSecretValue(t *testing.T) {
	secret := "sec\"ret\\tok\nline"
	r := NewRegistry()
	r.Add(secret)
	in, err := json.Marshal(map[string]string{"token": secret, "message": "prefix " + secret + " suffix"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := r.RedactJSON(in)
	assertValidJSON(t, out)
	for _, needle := range [][]byte{
		[]byte(secret),
		[]byte(escapedInner(secret, true)),
		[]byte(escapedInner(secret, false)),
	} {
		if bytes.Contains(out, needle) {
			t.Fatalf("escaped secret fragment %q survived in %s", needle, out)
		}
	}
	if bytes.Count(out, []byte(Token)) != 2 {
		t.Fatalf("redacted token count = %d in %s, want 2", bytes.Count(out, []byte(Token)), out)
	}
}

func TestRedactJSONDoesNotMatchAcrossStringBoundary(t *testing.T) {
	r := NewRegistry()
	r.Add(`abc","def`)
	in := []byte(`["abc","def"]`)
	out := r.RedactJSON(in)
	assertValidJSON(t, out)
	if !bytes.Equal(out, in) {
		t.Fatalf("structural bytes were altered: got %s want %s", out, in)
	}
}

func TestRedactJSONHandlesHTMLEscapedAndLiteralForms(t *testing.T) {
	r := NewRegistry()
	r.Add("alpha&beta")
	for _, in := range [][]byte{
		[]byte(`{"value":"alpha\u0026beta"}`),
		[]byte(`{"value":"alpha&beta"}`),
	} {
		out := r.RedactJSON(in)
		assertValidJSON(t, out)
		if bytes.Contains(out, []byte("alpha")) || !bytes.Contains(out, []byte(Token)) {
			t.Fatalf("HTML/literal form not redacted: %s", out)
		}
	}
}

func TestRedactJSONLeavesNonStringScalarsUntouched(t *testing.T) {
	r := NewRegistry()
	r.Add("12345")
	out := r.RedactJSON([]byte(`{"n":12345,"s":"12345"}`))
	assertValidJSON(t, out)
	if !bytes.Contains(out, []byte(`"n":12345`)) {
		t.Fatalf("numeric scalar changed: %s", out)
	}
	if bytes.Contains(out, []byte(`"s":"12345"`)) || !bytes.Contains(out, []byte(`"s":"`+Token+`"`)) {
		t.Fatalf("string scalar redaction wrong: %s", out)
	}
}

func TestRedactJSONDeterministicAcrossRegistrationOrder(t *testing.T) {
	input := []byte(`{"value":"secret-token-extended and secret-token"}`)
	first := NewRegistry()
	first.Add("secret-token")
	first.Add("secret-token-extended")
	second := NewRegistry()
	second.Add("secret-token-extended")
	second.Add("secret-token")

	gotFirst := first.RedactJSON(input)
	gotSecond := second.RedactJSON(input)
	assertValidJSON(t, gotFirst)
	if !bytes.Equal(gotFirst, gotSecond) {
		t.Fatalf("registration order changed JSON output:\nfirst  %s\nsecond %s", gotFirst, gotSecond)
	}
	if bytes.Contains(gotFirst, []byte("secret-token")) {
		t.Fatalf("overlapping secret survived: %s", gotFirst)
	}
}

func assertValidJSON(t *testing.T, b []byte) {
	t.Helper()
	if !json.Valid(b) {
		t.Fatalf("invalid JSON: %s", b)
	}
}
