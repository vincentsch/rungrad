package rungrad

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestClearSliceChangedRestoresReplaceSemanticsForPinnedPflagSliceFamily(t *testing.T) {
	// This table is intentionally built from real pflag values. If pflag changes
	// one value type's private shape, this test should fail before rungrad ships a
	// silently degraded reset path.
	tests := []struct {
		name     string
		first    string
		second   string
		want     []string
		register func(*pflag.FlagSet, string)
	}{
		{
			name:   "string_array",
			first:  "alpha",
			second: "beta",
			want:   []string{"beta"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []string
				fs.StringArrayVar(&v, name, []string{"default"}, "usage")
			},
		},
		{
			name:   "string_slice",
			first:  "alpha",
			second: "beta",
			want:   []string{"beta"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []string
				fs.StringSliceVar(&v, name, []string{"default"}, "usage")
			},
		},
		{
			name:   "bool_slice",
			first:  "true",
			second: "false",
			want:   []string{"false"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []bool
				fs.BoolSliceVar(&v, name, []bool{true}, "usage")
			},
		},
		{
			name:   "duration_slice",
			first:  "1s",
			second: "2s",
			want:   []string{"2s"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []time.Duration
				fs.DurationSliceVar(&v, name, []time.Duration{time.Second}, "usage")
			},
		},
		{
			name:   "float32_slice",
			first:  "1.25",
			second: "2.5",
			want:   []string{"2.500000"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []float32
				fs.Float32SliceVar(&v, name, []float32{1}, "usage")
			},
		},
		{
			name:   "float64_slice",
			first:  "1.25",
			second: "2.5",
			want:   []string{"2.500000"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []float64
				fs.Float64SliceVar(&v, name, []float64{1}, "usage")
			},
		},
		{
			name:   "int_slice",
			first:  "1",
			second: "2",
			want:   []string{"2"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []int
				fs.IntSliceVar(&v, name, []int{1}, "usage")
			},
		},
		{
			name:   "int32_slice",
			first:  "1",
			second: "2",
			want:   []string{"2"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []int32
				fs.Int32SliceVar(&v, name, []int32{1}, "usage")
			},
		},
		{
			name:   "int64_slice",
			first:  "1",
			second: "2",
			want:   []string{"2"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []int64
				fs.Int64SliceVar(&v, name, []int64{1}, "usage")
			},
		},
		{
			name:   "uint_slice",
			first:  "1",
			second: "2",
			want:   []string{"2"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []uint
				fs.UintSliceVar(&v, name, []uint{1}, "usage")
			},
		},
		{
			name:   "ip_slice",
			first:  "192.0.2.1",
			second: "192.0.2.2",
			want:   []string{"192.0.2.2"},
			register: func(fs *pflag.FlagSet, name string) {
				var v []net.IP
				fs.IPSliceVar(&v, name, []net.IP{net.ParseIP("192.0.2.10")}, "usage")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := pflag.NewFlagSet(tt.name, pflag.ContinueOnError)
			tt.register(fs, "value")
			if err := fs.Set("value", tt.first); err != nil {
				t.Fatalf("first Set(%q): %v", tt.first, err)
			}
			flag := fs.Lookup("value")
			if flag == nil {
				t.Fatal("registered flag not found")
			}
			if !sliceChangedFieldClearable(flag.Value) {
				t.Fatalf("pflag %s changed field is not clearable", tt.name)
			}
			if !clearSliceChanged(flag.Value) {
				t.Fatalf("clearSliceChanged(%s) returned false", tt.name)
			}
			if err := fs.Set("value", tt.second); err != nil {
				t.Fatalf("second Set(%q): %v", tt.second, err)
			}
			got := flag.Value.(pflag.SliceValue).GetSlice()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetSlice() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestIPNetSlicePinnedPflagShapeIsNotPublicSliceValue(t *testing.T) {
	// The p12 plan originally listed IPNetSlice with the SliceValue family, but
	// pflag v1.0.9 does not expose Append/Replace/GetSlice for it. Keep that fact
	// executable so a future pflag upgrade forces us to revisit the reset branch.
	_, def, err := net.ParseCIDR("10.0.9.0/24")
	if err != nil {
		t.Fatalf("parse default CIDR: %v", err)
	}
	fs := pflag.NewFlagSet("ipnet", pflag.ContinueOnError)
	var v []net.IPNet
	fs.IPNetSliceVar(&v, "value", []net.IPNet{*def}, "usage")

	flag := fs.Lookup("value")
	if flag == nil {
		t.Fatal("registered flag not found")
	}
	if _, ok := flag.Value.(pflag.SliceValue); ok {
		t.Fatal("pflag IPNetSlice unexpectedly implements SliceValue; add it to the slice reset canary")
	}

	if err := fs.Set("value", "10.0.0.0/24"); err != nil {
		t.Fatalf("first IPNetSlice Set: %v", err)
	}
	if !sliceChangedFieldClearable(flag.Value) {
		t.Fatal("pflag IPNetSlice changed field is not clearable")
	}
	if !clearSliceChanged(flag.Value) {
		t.Fatal("clearSliceChanged(IPNetSlice) returned false")
	}
	if err := fs.Set("value", "10.0.1.0/24"); err != nil {
		t.Fatalf("second IPNetSlice Set: %v", err)
	}
	got, err := fs.GetIPNetSlice("value")
	if err != nil {
		t.Fatalf("GetIPNetSlice: %v", err)
	}
	_, want, err := net.ParseCIDR("10.0.1.0/24")
	if err != nil {
		t.Fatalf("parse want CIDR: %v", err)
	}
	if len(got) != 1 || got[0].String() != want.String() {
		t.Fatalf("GetIPNetSlice() = %#v, want [%s]", got, want.String())
	}
}

func TestCloneStringsPreservesEmptyDefaults(t *testing.T) {
	if got := cloneStrings(nil); got != nil {
		t.Fatalf("cloneStrings(nil) = %#v, want nil", got)
	}

	empty := []string{}
	if got := cloneStrings(empty); got == nil || len(got) != 0 {
		t.Fatalf("cloneStrings(empty) = %#v, want nonnil empty slice", got)
	}

	in := []string{"a"}
	got := cloneStrings(in)
	in[0] = "b"
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("cloneStrings output aliases input: %#v", got)
	}
}

func TestClearSliceChangedFailsSafeOnForeignValue(t *testing.T) {
	fs := pflag.NewFlagSet("scalar", pflag.ContinueOnError)
	var b bool
	fs.BoolVar(&b, "bool", false, "usage")
	if clearSliceChanged(fs.Lookup("bool").Value) {
		t.Fatal("clearSliceChanged returned true for scalar bool flag")
	}

	foreign := &foreignSliceValue{values: []string{"a"}}
	if clearSliceChanged(foreign) {
		t.Fatal("clearSliceChanged returned true for foreign SliceValue")
	}
}

func TestResetSliceFlagFailsSafeWithoutChangedField(t *testing.T) {
	foreign := &foreignSliceValue{values: []string{"stale"}}
	flag := &pflag.Flag{Name: "foreign", Value: foreign, Changed: true}

	if resetSliceFlag(flag, foreign, []string{"default"}) {
		t.Fatal("resetSliceFlag returned true for foreign SliceValue")
	}
	if flag.Changed {
		t.Fatal("resetSliceFlag did not clear exported Changed field")
	}
	if got, want := foreign.GetSlice(), []string{"default"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("foreign value = %#v, want %#v", got, want)
	}
}

// foreignSliceValue is a minimal third-party SliceValue without pflag's private
// changed field. It proves resetSliceFlag degrades safely for non-pflag values.
type foreignSliceValue struct {
	values []string
}

var _ pflag.SliceValue = (*foreignSliceValue)(nil)

func (v *foreignSliceValue) String() string { return "" }
func (v *foreignSliceValue) Type() string   { return "foreignSlice" }

func (v *foreignSliceValue) Set(s string) error {
	v.values = []string{s}
	return nil
}

func (v *foreignSliceValue) Append(s string) error {
	v.values = append(v.values, s)
	return nil
}

func (v *foreignSliceValue) Replace(in []string) error {
	v.values = cloneStrings(in)
	return nil
}

func (v *foreignSliceValue) GetSlice() []string {
	return cloneStrings(v.values)
}
