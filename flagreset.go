package rungrad

import (
	"reflect"
	"unsafe"

	"github.com/spf13/pflag"
)

// sliceChangedField is pflag's unexported per-value "has been set once" tracker.
// Every pflag SliceValue type carries this bool; it decides whether the next Set
// replaces or appends. Pinned to pflag v1.0.9's field shape.
const sliceChangedField = "changed"

// cloneStrings returns an independent copy of in while preserving nil vs
// explicitly-empty slices.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// resetSliceFlag restores a pflag SliceValue flag to def with pristine change
// tracking. It fails safe with Replace + flag.Changed when the private bit cannot
// be reached, returning false instead of panicking.
func resetSliceFlag(flag *pflag.Flag, sv pflag.SliceValue, def []string) bool {
	_ = sv.Replace(cloneStrings(def))
	cleared := clearSliceChanged(flag.Value)
	flag.Changed = false
	return cleared
}

// clearSliceChanged sets pflag's unexported changed bit to false via an
// unsafe-backed reflect rebind. It returns false, without panicking, when the
// field is absent or not a settable bool.
func clearSliceChanged(v pflag.Value) bool {
	f := sliceChangedValue(v)
	if !f.IsValid() {
		return false
	}
	f.SetBool(false)
	return true
}

// sliceChangedFieldClearable reports whether pflag's unexported changed bit can
// be located and set for v under the pinned pflag dependency.
func sliceChangedFieldClearable(v pflag.Value) bool {
	return sliceChangedValue(v).IsValid()
}

// sliceChangedValue returns a settable reflect.Value aimed at pflag's unexported
// changed field, or the zero Value when the shape is not what v1.0.9 exposes. It
// never panics.
func sliceChangedValue(v pflag.Value) reflect.Value {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return reflect.Value{}
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	f := rv.FieldByName(sliceChangedField)
	if !f.IsValid() || f.Kind() != reflect.Bool || !f.CanAddr() {
		return reflect.Value{}
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}
