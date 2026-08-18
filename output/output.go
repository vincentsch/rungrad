// Package output renders a single command result model as either a human view
// or stable JSON, so the two can never drift. Every rungrad command builds one
// of these models and hands it to the framework, which picks the encoding based
// on the --json flag.
//
// The package is deliberately stdlib-only and deterministic: stable key and
// column ordering, no timestamps or random identifiers in default output, and a
// trailing newline on JSON. Those properties let any rungrad tool satisfy the
// determinism section of the spec for free.
package output

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
)

// Node is one labeled item in a result, optionally carrying nested children.
// A Node with Prose set is a guidance line for humans (a hint, a next step) and
// is omitted from the machine value.
type Node struct {
	Label    string `json:"label"`
	Value    any    `json:"value,omitempty"`
	Children []Node `json:"children,omitempty"`
	Prose    bool   `json:"-"`
}

// Table is a columnar result with a stable column order and an optional message
// shown to humans when there are no rows.
type Table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Empty   string     `json:"-"`
}

// MutationSummary describes the outcome of a write so that create, update, and
// delete commands report a consistent shape in both human and JSON form.
type MutationSummary struct {
	Action   string            `json:"action"`
	Resource string            `json:"resource"`
	Name     string            `json:"name,omitempty"`
	ID       string            `json:"id,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
	Notes    []string          `json:"notes,omitempty"`
}

// StableJSON encodes v deterministically: two-space indentation, alphabetical
// map keys (the encoding/json default), struct fields in declaration order, and
// a trailing newline. Two calls with equal input produce byte-identical output.
// A cyclic result model returns errCycle instead of recursing until the stack
// overflows.
func StableJSON(v any) ([]byte, error) {
	mv, err := machineValue(v)
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(mv, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

var (
	nodeType          = reflect.TypeOf(Node{})
	jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// errCycle is returned by StableJSON when the result model contains a reference
// cycle. The message is a constant, with no pointer address or map iteration
// detail, so two encodings of the same cyclic input produce byte-identical
// error text.
var errCycle = errors.New("cycle detected in result model")

// refKey identifies a pointer, map, or slice reference for stack-scoped cycle
// detection. kind and typ keep references of different kinds or types distinct;
// length and capacity keep distinct slice views from colliding.
type refKey struct {
	kind     reflect.Kind
	typ      reflect.Type
	ptr      uintptr
	length   int
	capacity int
}

// referenceKey builds the refKey for a pointer, map, or slice value. The generic
// Slice branch and machineNodes share it so the same []Node gets the same key
// through either route.
func referenceKey(v reflect.Value) refKey {
	k := refKey{kind: v.Kind(), typ: v.Type(), ptr: v.Pointer()}
	if v.Kind() == reflect.Slice {
		k.length = v.Len()
		k.capacity = v.Cap()
	}
	return k
}

// walkState holds the references on the active recursion path. It is created per
// StableJSON call and threaded through the walk; there is no package-level walk
// state.
type walkState struct {
	onPath map[refKey]struct{}
}

func newWalkState() *walkState { return &walkState{onPath: make(map[refKey]struct{})} }

// enter marks k as on the active path. It returns false if k is already present;
// callers must only call leave after a successful enter.
func (s *walkState) enter(k refKey) bool {
	if _, ok := s.onPath[k]; ok {
		return false
	}
	s.onPath[k] = struct{}{}
	return true
}

// leave unmarks k once the walk finishes the subtree it guards.
func (s *walkState) leave(k refKey) { delete(s.onPath, k) }

// machineValue converts a command result into the value that should be handed to
// encoding/json. It strips human-only Node prose first so JSON output never
// depends on the human renderer.
func machineValue(v any) (any, error) {
	out, omit, err := normalizeMachineValue(reflect.ValueOf(v), newWalkState())
	if err != nil {
		return nil, err
	}
	if omit || !out.IsValid() {
		return nil, nil
	}
	return out.Interface(), nil
}

// normalizeMachineValue walks v recursively and returns a replacement value plus
// an omit flag. It rebuilds containers instead of mutating caller-owned values,
// while path tracks references currently being walked so cycles return an error
// instead of recursing forever.
func normalizeMachineValue(v reflect.Value, path *walkState) (reflect.Value, bool, error) {
	if !v.IsValid() {
		return v, false, nil
	}
	// Interfaces do not own storage in the result model. Unwrap them and let the
	// concrete value decide whether it should be keyed, omitted, or copied.
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, false, nil
		}
		out, omit, err := normalizeMachineValue(v.Elem(), path)
		if err != nil {
			return reflect.Value{}, false, err
		}
		if omit {
			return reflect.Zero(v.Type()), true, nil
		}
		return out, false, nil
	}
	// Nodes are the only type with human-only fields. Handle them before custom
	// marshalers or generic structs so Prose, Value, and Children all share the
	// same machine-output rules.
	if v.Type() == nodeType {
		n := v.Interface().(Node)
		if n.Prose {
			return reflect.Zero(nodeType), true, nil
		}
		if n.Value != nil {
			// Value is any, so it can hide nested Nodes or reference cycles just like
			// any other part of a result model.
			nv, omit, err := normalizeMachineValue(reflect.ValueOf(n.Value), path)
			if err != nil {
				return reflect.Value{}, false, err
			}
			if omit || !nv.IsValid() {
				n.Value = nil
			} else {
				n.Value = nv.Interface()
			}
		}
		children, err := machineNodes(n.Children, path)
		if err != nil {
			return reflect.Value{}, false, err
		}
		n.Children = children
		return reflect.ValueOf(n), false, nil
	}
	// Custom encoders define their own JSON/text representation. Treat them as
	// leaves so private implementation details cannot affect StableJSON.
	if implementsCustomEncoding(v.Type()) {
		return v, false, nil
	}

	// Only pointers, slices, and maps can close a reference cycle. Structs and
	// arrays recurse normally; any cycle through them must pass through one of the
	// keyed reference kinds below.
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v, false, nil
		}
		key := referenceKey(v)
		if !path.enter(key) {
			return reflect.Value{}, false, errCycle
		}
		defer path.leave(key)
		elem, omit, err := normalizeMachineValue(v.Elem(), path)
		if err != nil {
			return reflect.Value{}, false, err
		}
		if omit {
			return reflect.Zero(v.Type()), true, nil
		}
		out := reflect.New(v.Type().Elem())
		if elem.Type().AssignableTo(v.Type().Elem()) {
			out.Elem().Set(elem)
		} else {
			out.Elem().Set(v.Elem())
		}
		return out, false, nil
	case reflect.Slice:
		if v.IsNil() {
			return v, false, nil
		}
		key := referenceKey(v)
		if !path.enter(key) {
			return reflect.Value{}, false, errCycle
		}
		defer path.leave(key)
		out := reflect.MakeSlice(v.Type(), 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			elem, omit, err := normalizeMachineValue(v.Index(i), path)
			if err != nil {
				return reflect.Value{}, false, err
			}
			if omit {
				continue
			}
			if elem.Type().AssignableTo(v.Type().Elem()) {
				out = reflect.Append(out, elem)
			} else {
				out = reflect.Append(out, v.Index(i))
			}
		}
		return out, false, nil
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			elem, omit, err := normalizeMachineValue(v.Index(i), path)
			if err != nil {
				return reflect.Value{}, false, err
			}
			if omit {
				elem = reflect.Zero(v.Type().Elem())
			}
			if elem.Type().AssignableTo(v.Type().Elem()) {
				out.Index(i).Set(elem)
			} else {
				out.Index(i).Set(v.Index(i))
			}
		}
		return out, false, nil
	case reflect.Map:
		if v.IsNil() {
			return v, false, nil
		}
		key := referenceKey(v)
		if !path.enter(key) {
			return reflect.Value{}, false, errCycle
		}
		defer path.leave(key)
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, mk := range v.MapKeys() {
			elem, omit, err := normalizeMachineValue(v.MapIndex(mk), path)
			if err != nil {
				return reflect.Value{}, false, err
			}
			if omit {
				continue
			}
			if elem.Type().AssignableTo(v.Type().Elem()) {
				out.SetMapIndex(mk, elem)
			} else {
				out.SetMapIndex(mk, v.MapIndex(mk))
			}
		}
		return out, false, nil
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			elem, omit, err := normalizeMachineValue(v.Field(i), path)
			if err != nil {
				return reflect.Value{}, false, err
			}
			if omit {
				elem = reflect.Zero(field.Type)
			}
			if elem.Type().AssignableTo(field.Type) {
				out.Field(i).Set(elem)
			} else {
				out.Field(i).Set(v.Field(i))
			}
		}
		return out, false, nil
	default:
		return v, false, nil
	}
}

// implementsCustomEncoding reports whether t controls its own JSON or text
// output. The pointer checks preserve pointer-receiver marshalers when a value
// reaches the walker.
func implementsCustomEncoding(t reflect.Type) bool {
	return t.Implements(jsonMarshalerType) ||
		t.Implements(textMarshalerType) ||
		(t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(jsonMarshalerType)) ||
		(t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(textMarshalerType))
}

// machineNodes normalizes a []Node while dropping prose nodes. It keys the slice
// it walks because Node.Children bypasses the generic Slice branch above.
func machineNodes(nodes []Node, path *walkState) ([]Node, error) {
	rv := reflect.ValueOf(nodes)
	if !rv.IsNil() {
		key := referenceKey(rv)
		if !path.enter(key) {
			return nil, errCycle
		}
		defer path.leave(key)
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		normalized, omit, err := normalizeMachineValue(reflect.ValueOf(n), path)
		if err != nil {
			return nil, err
		}
		if omit {
			continue
		}
		out = append(out, normalized.Interface().(Node))
	}
	return out, nil
}

// RenderNodes writes a human view of a node tree, indenting nested children.
func RenderNodes(w io.Writer, nodes []Node) {
	renderNodes(w, nodes, 0)
}

func renderNodes(w io.Writer, nodes []Node, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, n := range nodes {
		switch {
		case n.Prose:
			fmt.Fprintf(w, "%s%s\n", indent, n.Label)
		case n.Value == nil && len(n.Children) > 0:
			fmt.Fprintf(w, "%s%s:\n", indent, n.Label)
		default:
			fmt.Fprintf(w, "%s%s: %s\n", indent, n.Label, formatValue(n.Value))
		}
		if len(n.Children) > 0 {
			renderNodes(w, n.Children, depth+1)
		}
	}
}

// RenderTable writes an aligned, columnar human view of a table.
func RenderTable(w io.Writer, t Table) {
	if len(t.Rows) == 0 {
		msg := t.Empty
		if msg == "" {
			msg = "No results."
		}
		fmt.Fprintln(w, msg)
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	if len(t.Columns) > 0 {
		fmt.Fprintln(tw, strings.Join(t.Columns, "\t"))
	}
	for _, row := range t.Rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// RenderMutation writes a plain human view of a write outcome.
func RenderMutation(w io.Writer, m MutationSummary) { RenderMutationMode(w, m, TerminalMode{}) }

// RenderMutationMode writes a human view of a write outcome in the given
// terminal mode. Only a non-empty action word is colored; the resource, name,
// id, fields, and notes stay plain. An empty action stays escape-free even when
// styled.
func RenderMutationMode(w io.Writer, m MutationSummary, mode TerminalMode) {
	action := m.Action
	if mode.styled() && action != "" {
		action = ansiMutationAction + action + ansiReset
	}
	head := action
	if m.Resource != "" {
		head += " " + m.Resource
	}
	if m.Name != "" {
		head += " " + strconv(m.Name)
	}
	if m.ID != "" {
		head += " (" + m.ID + ")"
	}
	fmt.Fprintln(w, head)
	for _, k := range sortedKeys(m.Fields) {
		fmt.Fprintf(w, "  %s: %s\n", k, m.Fields[k])
	}
	for _, note := range m.Notes {
		fmt.Fprintf(w, "  %s\n", note)
	}
}

func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func strconv(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}
