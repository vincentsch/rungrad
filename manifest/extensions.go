package manifest

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ExtensionSet maps a product-owned namespace to its extension object.
type ExtensionSet map[string]ExtensionObject

// ExtensionObject is one namespace's JSON-object payload.
type ExtensionObject map[string]any

var namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*/[a-z0-9][a-z0-9._-]*$`)
var urlSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)
var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
var textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()

// coreCommandFieldNames are the manifest fields owned by rungrad. Extension
// objects may not reuse these names at their first level because that would make
// product metadata look like it was changing core command semantics.
var coreCommandFieldNames = map[string]bool{
	"path":                  true,
	"use":                   true,
	"short":                 true,
	"examples":              true,
	"related":               true,
	"output_modes":          true,
	"requires_auth":         true,
	"mutates":               true,
	"supports_dry_run":      true,
	"destructive":           true,
	"requires_confirmation": true,
	"supports_meta":         true,
	"local_flags":           true,
}

// extensionRefIdent identifies reference values during validation. Slice length
// and capacity are part of the key so a sub-slice is not mistaken for its
// parent.
type extensionRefIdent struct {
	kind     reflect.Kind
	typ      reflect.Type
	ptr      uintptr
	length   int
	capacity int
}

// ValidateExtensionSet checks that extensions are namespaced and JSON-compatible
// without colliding with rungrad-owned command fields.
func ValidateExtensionSet(set ExtensionSet) error {
	namespaces := make([]string, 0, len(set))
	for ns := range set {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	for _, ns := range namespaces {
		obj := set[ns]
		if !namespacePattern.MatchString(ns) {
			return fmt.Errorf("extension namespace %q: invalid namespace", ns)
		}
		if strings.HasPrefix(ns, "rungrad/") || strings.HasPrefix(ns, "rungrad.") {
			return fmt.Errorf("extension namespace %q: reserved namespace", ns)
		}
		if obj == nil {
			return fmt.Errorf("extension namespace %q: null value", ns)
		}
		if len(obj) == 0 {
			continue
		}

		fields := make([]string, 0, len(obj))
		for field := range obj {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			if coreCommandFieldNames[field] {
				return fmt.Errorf("extension namespace %q field %q: core field contradiction", ns, field)
			}
			if err := validateExtensionValue(reflect.ValueOf(obj[field]), ns+"."+field, map[extensionRefIdent]bool{}); err != nil {
				return err
			}
		}
	}
	return nil
}

// EncodeExtensions validates extensions and returns their canonical compact JSON
// annotation form. Empty extension sets encode to an absent annotation.
func EncodeExtensions(set ExtensionSet) (string, error) {
	if len(set) == 0 {
		return "", nil
	}
	if err := ValidateExtensionSet(set); err != nil {
		return "", err
	}
	filtered := ExtensionSet{}
	for ns, obj := range set {
		if len(obj) == 0 {
			continue
		}
		filtered[ns] = obj
	}
	if len(filtered) == 0 {
		return "", nil
	}
	b, err := json.Marshal(filtered)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeExtensions parses the rungrad.extensions annotation, preserving JSON
// numbers and rejecting duplicate object keys before validation.
func DecodeExtensions(s string) (ExtensionSet, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	set, err := parseExtensionJSON(s)
	if err != nil {
		return nil, err
	}
	if err := ValidateExtensionSet(set); err != nil {
		return nil, err
	}
	return set, nil
}

// UnmarshalJSON preserves JSON numbers and rejects duplicate keys when an
// ExtensionSet appears inside a manifest decoded from wire JSON. Semantic shape
// validation still runs from Validate, matching the rest of the manifest schema.
func (set *ExtensionSet) UnmarshalJSON(b []byte) error {
	parsed, err := parseExtensionJSON(string(b))
	if err != nil {
		return err
	}
	*set = parsed
	return nil
}

func parseExtensionJSON(s string) (ExtensionSet, error) {
	// encoding/json keeps the last duplicate object key. Extensions are contract
	// data, so parse them with a token scanner that rejects duplicates before
	// converting the top-level object to ExtensionSet.
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	raw, err := decodeNoDup(dec, "")
	if err != nil {
		return nil, err
	}
	if tok, err := dec.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("extensions: trailing data after top-level value %v", tok)
	}

	rawObj, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("extensions must be a JSON object")
	}
	set := make(ExtensionSet, len(rawObj))
	for ns, value := range rawObj {
		if value == nil {
			return nil, fmt.Errorf("extension namespace %q: null value", ns)
		}
		obj, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("extension namespace %q: non-object namespace value", ns)
		}
		set[ns] = ExtensionObject(obj)
	}
	return set, nil
}

// RequireExtensionFields verifies that a namespace contains non-empty fields.
func RequireExtensionFields(set ExtensionSet, namespace string, fields ...string) error {
	obj, ok := set[namespace]
	if !ok {
		return fmt.Errorf("extension namespace %q: required namespace is missing", namespace)
	}
	for _, field := range fields {
		value, ok := obj[field]
		if !ok || !isPresentNonEmpty(reflect.ValueOf(value)) {
			return fmt.Errorf("extension namespace %q field %q: required field is empty or missing", namespace, field)
		}
	}
	return nil
}

// RequireExtensionEnum verifies that a field is a string in the allowed set.
func RequireExtensionEnum(set ExtensionSet, namespace, field string, allowed ...string) error {
	value, ok, err := extensionField(set, namespace, field)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("extension namespace %q field %q: required field is empty or missing", namespace, field)
	}
	got, ok := value.(string)
	if !ok {
		return fmt.Errorf("extension namespace %q field %q: must be a string", namespace, field)
	}
	for _, want := range allowed {
		if got == want {
			return nil
		}
	}
	return fmt.Errorf("extension namespace %q field %q: value %q not in allowed set %v", namespace, field, got, allowed)
}

// RequireExtensionDocPath verifies that a field is a clean, relative docs path.
func RequireExtensionDocPath(set ExtensionSet, namespace, field string) error {
	value, ok, err := extensionField(set, namespace, field)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("extension namespace %q field %q: required field is empty or missing", namespace, field)
	}
	p, ok := value.(string)
	if !ok {
		return fmt.Errorf("extension namespace %q field %q: must be a string", namespace, field)
	}
	if p == "" {
		return fmt.Errorf("extension namespace %q field %q: required field is empty or missing", namespace, field)
	}
	if path.IsAbs(p) {
		return fmt.Errorf("extension namespace %q field %q: must be a relative path", namespace, field)
	}
	if strings.Contains(p, "://") || urlSchemePattern.MatchString(p) {
		return fmt.Errorf("extension namespace %q field %q: must not be a URL", namespace, field)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("extension namespace %q field %q: must be a clean path", namespace, field)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return fmt.Errorf("extension namespace %q field %q: must not escape via ..", namespace, field)
		}
	}
	return nil
}

func validateExtensionValue(rv reflect.Value, label string, onPath map[extensionRefIdent]bool) error {
	if !rv.IsValid() {
		return fmt.Errorf("%s: null value", label)
	}
	for rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return fmt.Errorf("%s: null value", label)
		}
		rv = rv.Elem()
	}

	if rv.CanInterface() {
		// json.Number is a string kind, but it represents a JSON number on the
		// wire. Validate its numeric spelling before the generic string case.
		if n, ok := rv.Interface().(json.Number); ok {
			b, err := json.Marshal(n)
			if err != nil {
				return fmt.Errorf("%s: invalid number %q", label, n)
			}
			if _, err := strconv.ParseFloat(string(b), 64); err != nil {
				return fmt.Errorf("%s: invalid number %q", label, n)
			}
			f, _ := strconv.ParseFloat(n.String(), 64)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return fmt.Errorf("%s: non-finite number", label)
			}
			return nil
		}
		// Extension values are limited to plain JSON-shaped Go data. Custom
		// marshalers can encode into a different shape than the validator just
		// walked, so reject them before json.Marshal sees them.
		if err := rejectExtensionCustomMarshaler(rv, label); err != nil {
			return err
		}
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return fmt.Errorf("%s: null value", label)
		}
		if done, err := enterExtensionRef(rv, label, onPath); done || err != nil {
			return err
		}
		defer leaveExtensionRef(rv, onPath)
		return validateExtensionValue(rv.Elem(), label, onPath)
	case reflect.Bool, reflect.String:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%s: non-finite number", label)
		}
		return nil
	case reflect.Slice:
		if rv.IsNil() {
			return fmt.Errorf("%s: null value", label)
		}
		if done, err := enterExtensionRef(rv, label, onPath); done || err != nil {
			return err
		}
		defer leaveExtensionRef(rv, onPath)
		for i := 0; i < rv.Len(); i++ {
			if err := validateExtensionValue(rv.Index(i), fmt.Sprintf("%s[%d]", label, i), onPath); err != nil {
				return err
			}
		}
		return nil
	case reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := validateExtensionValue(rv.Index(i), fmt.Sprintf("%s[%d]", label, i), onPath); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if rv.IsNil() {
			return fmt.Errorf("%s: null value", label)
		}
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%s: non-string map key", label)
		}
		if done, err := enterExtensionRef(rv, label, onPath); done || err != nil {
			return err
		}
		defer leaveExtensionRef(rv, onPath)
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, keyValue := range keys {
			key := keyValue.String()
			if err := validateExtensionValue(rv.MapIndex(keyValue), label+"."+key, onPath); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: unsupported value type %s", label, rv.Kind())
	}
}

// rejectExtensionCustomMarshaler rejects values whose wire form is controlled
// by methods rather than by the reflected JSON shape validated above.
func rejectExtensionCustomMarshaler(rv reflect.Value, label string) error {
	if isNilableExtensionKind(rv.Kind()) && rv.IsNil() {
		return nil
	}
	t := rv.Type()
	if typeOrPointerImplements(t, jsonMarshalerType) {
		return fmt.Errorf("%s: custom JSON marshaler unsupported", label)
	}
	if typeOrPointerImplements(t, textMarshalerType) {
		return fmt.Errorf("%s: custom text marshaler unsupported", label)
	}
	return nil
}

// typeOrPointerImplements matches the method-set cases that encoding/json can
// use, including pointer-receiver marshalers on concrete values.
func typeOrPointerImplements(t, iface reflect.Type) bool {
	if t.Implements(iface) {
		return true
	}
	if t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(iface) {
		return true
	}
	return false
}

// isNilableExtensionKind protects callers from calling IsNil on scalar values.
func isNilableExtensionKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}

// enterExtensionRef records a pointer-like value on the current recursion path.
// The tracking is stack-scoped: shared acyclic values are allowed, but a value
// that points back to an ancestor is rejected as a cycle.
func enterExtensionRef(rv reflect.Value, label string, onPath map[extensionRefIdent]bool) (bool, error) {
	id := extensionRefKey(rv)
	if id.ptr == 0 {
		return false, nil
	}
	if onPath[id] {
		return true, fmt.Errorf("%s: cycle detected", label)
	}
	onPath[id] = true
	return false, nil
}

func leaveExtensionRef(rv reflect.Value, onPath map[extensionRefIdent]bool) {
	id := extensionRefKey(rv)
	if id.ptr != 0 {
		delete(onPath, id)
	}
}

// extensionRefKey distinguishes slice views by length and capacity. Two
// sub-slices can share the same first-element pointer without forming a cycle.
func extensionRefKey(rv reflect.Value) extensionRefIdent {
	id := extensionRefIdent{kind: rv.Kind(), typ: rv.Type(), ptr: rv.Pointer()}
	if rv.Kind() == reflect.Slice {
		id.length = rv.Len()
		id.capacity = rv.Cap()
	}
	return id
}

// decodeNoDup is a small JSON token reader that preserves json.Number values
// through dec.UseNumber and rejects duplicate object keys at every depth.
func decodeNoDup(dec *json.Decoder, label string) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("extensions: object key at %q is not a string", label)
			}
			child := joinExtensionPath(label, key)
			if seen[key] {
				return nil, fmt.Errorf("extensions: duplicate key %q", child)
			}
			seen[key] = true
			value, err := decodeNoDup(dec, child)
			if err != nil {
				return nil, err
			}
			obj[key] = value
		}
		if tok, err := dec.Token(); err != nil {
			return nil, err
		} else if tok != json.Delim('}') {
			return nil, fmt.Errorf("extensions: expected object end at %q", label)
		}
		return obj, nil
	case '[':
		arr := []any{}
		for i := 0; dec.More(); i++ {
			value, err := decodeNoDup(dec, fmt.Sprintf("%s[%d]", label, i))
			if err != nil {
				return nil, err
			}
			arr = append(arr, value)
		}
		if tok, err := dec.Token(); err != nil {
			return nil, err
		} else if tok != json.Delim(']') {
			return nil, fmt.Errorf("extensions: expected array end at %q", label)
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("extensions: unexpected delimiter %q", delim)
	}
}

func joinExtensionPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func extensionField(set ExtensionSet, namespace, field string) (any, bool, error) {
	obj, ok := set[namespace]
	if !ok {
		return nil, false, fmt.Errorf("extension namespace %q: required namespace is missing", namespace)
	}
	value, ok := obj[field]
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

// isPresentNonEmpty implements the helper-level meaning of "required": scalar
// values count as present, while strings, arrays, and objects must be non-empty.
func isPresentNonEmpty(rv reflect.Value) bool {
	if !rv.IsValid() {
		return false
	}
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if rv.CanInterface() {
		if _, ok := rv.Interface().(json.Number); ok {
			return true
		}
	}
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	case reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
