// Package redact maintains a per-run registry of secret values and removes them
// from outbound bytes at the framework's output boundaries. Registration is
// dynamic: auth/config code and command handlers add values discovered at
// runtime, and the framework redacts them from success output, metadata,
// transforms, previews, errors, and informational text.
//
// The package is stdlib-only and deterministic: redaction is a pure function of
// the registered set and the input bytes, independent of registration order.
package redact

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// Token replaces every redacted occurrence. It contains no JSON string
// metacharacters, so substituting it inside a JSON string literal keeps the
// document valid.
const Token = "[REDACTED]"

// minLen avoids replacing short common words or punctuation fragments. The
// framework treats real credentials as opaque strings longer than this.
const minLen = 5

var tokenBytes = []byte(Token)

// Registry holds secret values for one run. The zero value is ready to use. It
// is not safe for concurrent registration, matching rungrad's one-run-per-
// Factory model.
type Registry struct {
	secrets    map[string]struct{}
	textCached [][]byte
	jsonCached [][]byte
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Reset forgets every registered secret.
func (r *Registry) Reset() {
	if r == nil {
		return
	}
	r.secrets, r.textCached, r.jsonCached = nil, nil, nil
}

// Add registers an exact secret value. Empty, whitespace-only, and values
// shorter than five bytes are ignored. Re-adding a value is a no-op.
func (r *Registry) Add(value string) {
	if r == nil || len(value) < minLen || strings.TrimSpace(value) == "" {
		return
	}
	if r.secrets == nil {
		r.secrets = make(map[string]struct{})
	}
	if _, ok := r.secrets[value]; ok {
		return
	}
	r.secrets[value] = struct{}{}
	r.textCached, r.jsonCached = nil, nil
}

// textNeedles covers free-text boundaries. Text can contain either a raw secret
// or a JSON-escaped spelling copied from an upstream response, so both forms are
// matched.
func (r *Registry) textNeedles() [][]byte {
	if r.textCached != nil {
		return r.textCached
	}
	set := make(map[string]struct{}, len(r.secrets)*3)
	for s := range r.secrets {
		set[s] = struct{}{}
		set[escapedInner(s, true)] = struct{}{}
		set[escapedInner(s, false)] = struct{}{}
	}
	r.textCached = sortedNeedles(set)
	return r.textCached
}

// jsonNeedles deliberately excludes the raw secret. Matching raw bytes in JSON
// would let a secret containing quotes consume structural JSON outside a string.
func (r *Registry) jsonNeedles() [][]byte {
	if r.jsonCached != nil {
		return r.jsonCached
	}
	set := make(map[string]struct{}, len(r.secrets)*2)
	for s := range r.secrets {
		set[escapedInner(s, true)] = struct{}{}
		set[escapedInner(s, false)] = struct{}{}
	}
	r.jsonCached = sortedNeedles(set)
	return r.jsonCached
}

// sortedNeedles makes redaction independent of registration order. Longest
// first prevents a short secret from partially replacing a longer overlapping
// one before the longer value gets a chance to match.
func sortedNeedles(set map[string]struct{}) [][]byte {
	out := make([][]byte, 0, len(set))
	for n := range set {
		if n != "" {
			out = append(out, []byte(n))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return bytes.Compare(out[i], out[j]) < 0
	})
	return out
}

// RedactBytes replaces every registered secret in text output, matching both
// raw and escaped forms so a secret cannot survive in either shape.
func (r *Registry) RedactBytes(b []byte) []byte {
	if r == nil || len(b) == 0 {
		return b
	}
	needles := r.textNeedles()
	if len(needles) == 0 {
		return b
	}
	for _, needle := range needles {
		b = bytes.ReplaceAll(b, needle, tokenBytes)
	}
	return b
}

// RedactString is RedactBytes over a string.
func (r *Registry) RedactString(s string) string {
	if r == nil || s == "" {
		return s
	}
	return string(r.RedactBytes([]byte(s)))
}

// RedactJSON removes registered secrets from JSON output while keeping the
// document valid. It rewrites only bytes inside string literals, using the
// JSON-escaped forms of each secret, so structural bytes and non-string scalars
// are never altered.
func (r *Registry) RedactJSON(b []byte) []byte {
	if r == nil || len(b) == 0 {
		return b
	}
	needles := r.jsonNeedles()
	if len(needles) == 0 {
		return b
	}
	return redactInJSONStrings(b, needles)
}

// escapedInner returns exactly how s appears between the quotes of a JSON string.
// Both HTML-escaped and non-HTML forms are needed because encoding/json and gojq
// differ on characters such as <, >, and &.
func escapedInner(s string, escapeHTML bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(s); err != nil {
		return s
	}
	out := strings.TrimRight(buf.String(), "\n")
	if len(out) >= 2 && out[0] == '"' && out[len(out)-1] == '"' {
		return out[1 : len(out)-1]
	}
	return out
}

// redactInJSONStrings rewrites only string literal contents. That keeps numbers,
// booleans, nulls, object punctuation, and array punctuation unchanged, so the
// output remains valid JSON even when a registered secret contains quotes.
func redactInJSONStrings(b []byte, needles [][]byte) []byte {
	out := make([]byte, 0, len(b))
	inString := false
	for i := 0; i < len(b); {
		c := b[i]
		if !inString {
			out = append(out, c)
			if c == '"' {
				inString = true
			}
			i++
			continue
		}

		// Try a full escaped-secret match before treating a backslash as a JSON
		// escape. Secrets can begin with an escaped quote or control character.
		matched := false
		for _, needle := range needles {
			if bytes.HasPrefix(b[i:], needle) {
				out = append(out, tokenBytes...)
				i += len(needle)
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		switch {
		case c == '\\':
			// Copy escapes as indivisible units so matching never starts in the
			// middle of \uXXXX or a two-byte escape.
			out = append(out, c)
			i++
			if i < len(b) {
				if b[i] == 'u' && i+4 < len(b) {
					out = append(out, b[i:i+5]...)
					i += 5
				} else {
					out = append(out, b[i])
					i++
				}
			}
		case c == '"':
			out = append(out, c)
			inString = false
			i++
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}
