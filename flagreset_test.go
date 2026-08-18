package rungrad_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	rungrad "github.com/vincentsch/rungrad"
	"github.com/vincentsch/rungrad/testutil"
)

// sliceSnapshot is the stable JSON model emitted by the fixture commands.
type sliceSnapshot struct {
	PArr   []string `json:"p_arr"`
	PSlice []string `json:"p_slice"`
	RArr   []string `json:"r_arr"`
	RSlice []string `json:"r_slice"`
	LArr   []string `json:"l_arr"`
	LSlice []string `json:"l_slice"`
	Loud   bool     `json:"loud"`
	Name   string   `json:"name"`
}

// sliceEcho holds the actual variables bound to Cobra flags. Handlers emit a
// copied sliceSnapshot so tests assert what command code receives, not a later
// value after another reset mutates the bound variables.
type sliceEcho struct {
	PArr   []string
	PSlice []string
	RArr   []string
	RSlice []string
	LArr   []string
	LSlice []string
	Loud   bool
	Name   string

	completion    sliceSnapshot
	completionSet bool
}

// snapshot preserves nil versus empty slices while detaching the JSON model from
// the flag variables that the reused App will reset again on the next run.
func (e *sliceEcho) snapshot() sliceSnapshot {
	return sliceSnapshot{
		PArr:   copyTestStrings(e.PArr),
		PSlice: copyTestStrings(e.PSlice),
		RArr:   copyTestStrings(e.RArr),
		RSlice: copyTestStrings(e.RSlice),
		LArr:   copyTestStrings(e.LArr),
		LSlice: copyTestStrings(e.LSlice),
		Loud:   e.Loud,
		Name:   e.Name,
	}
}

// newSliceFixtureApp builds one command tree that exercises the reset lifecycle
// at each Cobra flag scope: root-local, root-persistent, leaf-local, inherited
// persistent, and nested inherited persistent.
func newSliceFixtureApp(arrDef, sliceDef []string) (*rungrad.App, *sliceEcho) {
	app := rungrad.New(rungrad.AppConfig{
		Name:    "sliceflags",
		Short:   "slice flag test",
		Version: "0.0.0-test",
	})
	echo := &sliceEcho{}

	root := app.Root()
	root.PersistentFlags().StringArrayVar(&echo.PArr, "p-arr", copyTestStrings(arrDef), "Persistent array")
	root.PersistentFlags().StringSliceVar(&echo.PSlice, "p-slice", copyTestStrings(sliceDef), "Persistent slice")
	root.Flags().StringArrayVar(&echo.RArr, "r-arr", copyTestStrings(arrDef), "Root array")
	root.Flags().StringSliceVar(&echo.RSlice, "r-slice", copyTestStrings(sliceDef), "Root slice")
	root.Flags().BoolVar(&echo.Loud, "loud", false, "Use loud mode")
	root.Flags().StringVar(&echo.Name, "name", "anon", "Name")
	root.RunE = func(cmd *cobra.Command, args []string) error {
		return app.Factory().WriteResult(echo.snapshot(), func(io.Writer) {})
	}

	write := func(f *rungrad.Factory) error {
		return f.WriteResult(echo.snapshot(), func(io.Writer) {})
	}
	leaf := &rungrad.Command{
		Use:   "leaf",
		Short: "leaf command",
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringArrayVar(&echo.LArr, "l-arr", copyTestStrings(arrDef), "Leaf array")
			cmd.Flags().StringSliceVar(&echo.LSlice, "l-slice", copyTestStrings(sliceDef), "Leaf slice")
			cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				// Completion runs through RunIO and resetForRun but does not execute a
				// handler, so capture the bound variables here before any later run can
				// reset them again.
				echo.completion = echo.snapshot()
				echo.completionSet = true
				return []string{"done"}, cobra.ShellCompDirectiveNoFileComp
			}
		},
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return write(f)
		},
	}
	child := &rungrad.Command{
		Use:   "child",
		Short: "child command",
		Run: func(f *rungrad.Factory, cmd *cobra.Command, args []string) error {
			return write(f)
		},
	}
	parent := &rungrad.Command{Use: "parent", Short: "parent command"}
	parent.AddCommand(child)
	app.AddCommand(leaf, parent)

	return app, echo
}

func runSliceJSON(t *testing.T, app *rungrad.App, args ...string) (sliceSnapshot, testutil.Result) {
	t.Helper()
	all := append([]string{"--json"}, args...)
	res := testutil.Run(app, all...)
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("%v exit %d stderr=%q stdout=%q", all, res.Exit, res.Stderr, res.Stdout)
	}
	var got sliceSnapshot
	if err := res.JSON(&got); err != nil {
		t.Fatalf("%v stdout is not slice JSON: %v\n%s", all, err, res.Stdout)
	}
	return got, res
}

// copyTestStrings preserves nil versus empty slices, matching the production
// default snapshot behavior that these tests are asserting.
func copyTestStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func requireSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

// requireJSONEmptyArray pins empty defaults as JSON [] rather than null.
func requireJSONEmptyArray(t *testing.T, field, stdout string) {
	t.Helper()
	if !strings.Contains(stdout, `"`+field+`": []`) {
		t.Fatalf("%s should be encoded as [], got:\n%s", field, stdout)
	}
}

func TestStringArrayEmptyDefaultNoMarkerLeak(t *testing.T) {
	app, _ := newSliceFixtureApp(nil, nil)
	got, _ := runSliceJSON(t, app, "leaf", "--l-arr", "a", "--l-arr", "b")
	requireSlice(t, "l_arr", got.LArr, []string{"a", "b"})
}

func TestStringArrayLiteralBracketSurvives(t *testing.T) {
	app, _ := newSliceFixtureApp(nil, nil)
	got, _ := runSliceJSON(t, app, "leaf", "--l-arr", "[]")
	requireSlice(t, "l_arr", got.LArr, []string{"[]"})
}

func TestStringSliceLiteralBracketSurvives(t *testing.T) {
	app, _ := newSliceFixtureApp(nil, nil)
	got, _ := runSliceJSON(t, app, "leaf", "--l-slice", "[]")
	requireSlice(t, "l_slice", got.LSlice, []string{"[]"})
}

func TestStringArraySliceFlagsResetMatrix(t *testing.T) {
	defaults := []struct {
		name string
		def  []string
	}{
		{name: "nil_default", def: nil},
		{name: "empty_default", def: []string{}},
		{name: "nonempty_default", def: []string{"x", "y"}},
	}
	inputs := []struct {
		name string
		args func(flag string) []string
		want func(def []string) []string
	}{
		{
			name: "none",
			args: func(string) []string { return nil },
			want: func(def []string) []string {
				if len(def) == 0 {
					return []string{}
				}
				return copyTestStrings(def)
			},
		},
		{
			name: "one_value",
			args: func(flag string) []string { return []string{"--" + flag, "a"} },
			want: func([]string) []string { return []string{"a"} },
		},
		{
			name: "repeated_values",
			args: func(flag string) []string { return []string{"--" + flag, "a", "--" + flag, "b"} },
			want: func([]string) []string { return []string{"a", "b"} },
		},
		{
			name: "literal_brackets",
			args: func(flag string) []string { return []string{"--" + flag, "[]"} },
			want: func([]string) []string { return []string{"[]"} },
		},
	}
	targets := []struct {
		name      string
		flag      string
		jsonField string
		value     func(sliceSnapshot) []string
	}{
		{name: "leaf_local", flag: "l-arr", jsonField: "l_arr", value: func(s sliceSnapshot) []string { return s.LArr }},
		{name: "root_persistent_inherited", flag: "p-arr", jsonField: "p_arr", value: func(s sliceSnapshot) []string { return s.PArr }},
	}

	for _, def := range defaults {
		for _, target := range targets {
			for _, input := range inputs {
				t.Run(def.name+"/"+target.name+"/"+input.name, func(t *testing.T) {
					app, _ := newSliceFixtureApp(def.def, nil)
					args := append([]string{"leaf"}, input.args(target.flag)...)
					got, res := runSliceJSON(t, app, args...)
					requireSlice(t, target.jsonField, target.value(got), input.want(def.def))
					if def.name == "empty_default" && input.name == "none" {
						requireJSONEmptyArray(t, target.jsonField, res.Stdout)
					}
				})
			}
		}
	}
}

func TestStringSliceSliceFlagsResetMatrix(t *testing.T) {
	defaults := []struct {
		name string
		def  []string
	}{
		{name: "nil_default", def: nil},
		{name: "empty_default", def: []string{}},
		{name: "nonempty_default", def: []string{"x", "y"}},
	}
	inputs := []struct {
		name string
		args func(flag string) []string
		want func(def []string) []string
	}{
		{
			name: "none",
			args: func(string) []string { return nil },
			want: func(def []string) []string { return copyTestStrings(def) },
		},
		{
			name: "one_value",
			args: func(flag string) []string { return []string{"--" + flag, "a"} },
			want: func([]string) []string { return []string{"a"} },
		},
		{
			name: "repeated_values",
			args: func(flag string) []string { return []string{"--" + flag, "a,b", "--" + flag, "c"} },
			want: func([]string) []string { return []string{"a", "b", "c"} },
		},
		{
			name: "literal_brackets",
			args: func(flag string) []string { return []string{"--" + flag, "[]"} },
			want: func([]string) []string { return []string{"[]"} },
		},
	}
	targets := []struct {
		name      string
		flag      string
		jsonField string
		value     func(sliceSnapshot) []string
	}{
		{name: "leaf_local", flag: "l-slice", jsonField: "l_slice", value: func(s sliceSnapshot) []string { return s.LSlice }},
		{name: "root_persistent_inherited", flag: "p-slice", jsonField: "p_slice", value: func(s sliceSnapshot) []string { return s.PSlice }},
	}

	for _, def := range defaults {
		for _, target := range targets {
			for _, input := range inputs {
				t.Run(def.name+"/"+target.name+"/"+input.name, func(t *testing.T) {
					app, _ := newSliceFixtureApp(nil, def.def)
					args := append([]string{"leaf"}, input.args(target.flag)...)
					got, res := runSliceJSON(t, app, args...)
					requireSlice(t, target.jsonField, target.value(got), input.want(def.def))
					if def.name == "empty_default" && input.name == "none" {
						requireJSONEmptyArray(t, target.jsonField, res.Stdout)
					}
				})
			}
		}
	}
}

func TestSliceFlagsReusedAppReplacesDefault(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{"x", "y"}, []string{"x", "y"})

	got, _ := runSliceJSON(t, app, "leaf", "--l-arr", "a", "--l-slice", "a")
	requireSlice(t, "l_arr run 1", got.LArr, []string{"a"})
	requireSlice(t, "l_slice run 1", got.LSlice, []string{"a"})

	got, _ = runSliceJSON(t, app, "leaf", "--l-arr", "b", "--l-slice", "b")
	requireSlice(t, "l_arr run 2", got.LArr, []string{"b"})
	requireSlice(t, "l_slice run 2", got.LSlice, []string{"b"})

	got, _ = runSliceJSON(t, app, "leaf")
	requireSlice(t, "l_arr run 3", got.LArr, []string{"x", "y"})
	requireSlice(t, "l_slice run 3", got.LSlice, []string{"x", "y"})
}

func TestSliceFlagsReusedAppEmptyDefaultDoesNotGrow(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{}, []string{})

	got, _ := runSliceJSON(t, app, "leaf", "--l-arr", "a", "--l-arr", "b", "--l-slice", "a", "--l-slice", "b")
	requireSlice(t, "l_arr run 1", got.LArr, []string{"a", "b"})
	requireSlice(t, "l_slice run 1", got.LSlice, []string{"a", "b"})

	got, res := runSliceJSON(t, app, "leaf")
	requireSlice(t, "l_arr run 2", got.LArr, []string{})
	requireSlice(t, "l_slice run 2", got.LSlice, []string{})
	requireJSONEmptyArray(t, "l_arr", res.Stdout)
	requireJSONEmptyArray(t, "l_slice", res.Stdout)
}

func TestRootCommandSliceFlagsResetDirectly(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{"x", "y"}, []string{"s", "t"})

	got, _ := runSliceJSON(t, app, "--r-arr", "a", "--r-slice", "m,n", "--p-arr", "p")
	requireSlice(t, "r_arr run 1", got.RArr, []string{"a"})
	requireSlice(t, "r_slice run 1", got.RSlice, []string{"m", "n"})
	requireSlice(t, "p_arr run 1", got.PArr, []string{"p"})

	got, _ = runSliceJSON(t, app)
	requireSlice(t, "r_arr run 2", got.RArr, []string{"x", "y"})
	requireSlice(t, "r_slice run 2", got.RSlice, []string{"s", "t"})
	requireSlice(t, "p_arr run 2", got.PArr, []string{"x", "y"})
	requireSlice(t, "p_slice run 2", got.PSlice, []string{"s", "t"})
}

func TestNestedInheritedPersistentSliceReset(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{"x", "y"}, []string{"s", "t"})

	got, _ := runSliceJSON(t, app, "--p-arr", "a", "parent", "child")
	requireSlice(t, "p_arr run 1", got.PArr, []string{"a"})

	got, _ = runSliceJSON(t, app, "parent", "child")
	requireSlice(t, "p_arr run 2", got.PArr, []string{"x", "y"})
	requireSlice(t, "p_slice run 2", got.PSlice, []string{"s", "t"})
}

func TestScalarFlagsUnaffectedBySliceFix(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{}, nil)

	got, _ := runSliceJSON(t, app, "--loud", "--name", "x", "--r-arr", "a")
	if !got.Loud || got.Name != "x" {
		t.Fatalf("first run scalar values = loud:%v name:%q, want loud:true name:x", got.Loud, got.Name)
	}
	requireSlice(t, "r_arr run 1", got.RArr, []string{"a"})

	got, _ = runSliceJSON(t, app)
	if got.Loud || got.Name != "anon" {
		t.Fatalf("second run scalar values = loud:%v name:%q, want loud:false name:anon", got.Loud, got.Name)
	}
	requireSlice(t, "r_arr run 2", got.RArr, []string{})
}

func TestManifestEmissionIndependentOfSliceValue(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{"x", "y"}, nil)

	runSliceJSON(t, app, "leaf", "--l-arr", "a", "--l-arr", "b")
	m, first := readManifest(t, app)
	_, second := readManifest(t, app)
	if first.Stdout != second.Stdout {
		t.Fatalf("manifest output not repeatable:\n%s\n---\n%s", first.Stdout, second.Stdout)
	}
	leaf := findManifestCommand(&m, "leaf")
	if leaf == nil {
		t.Fatal("leaf command missing from manifest")
	}
	flag := findManifestFlag(leaf.LocalFlags, "l-arr")
	if flag == nil {
		t.Fatal("leaf l-arr flag missing from manifest")
	}
	if flag.Default != "[x,y]" {
		t.Fatalf("manifest l-arr default = %q, want %q", flag.Default, "[x,y]")
	}
}

func TestHelpDefaultIndependentOfSliceValue(t *testing.T) {
	app, _ := newSliceFixtureApp([]string{"x", "y"}, nil)

	before := testutil.Run(app, "leaf", "--help")
	if before.Exit != rungrad.ExitSuccess {
		t.Fatalf("help before value run exit %d stderr=%q", before.Exit, before.Stderr)
	}
	runSliceJSON(t, app, "leaf", "--l-arr", "a")
	after := testutil.Run(app, "leaf", "--help")
	if after.Exit != rungrad.ExitSuccess {
		t.Fatalf("help after value run exit %d stderr=%q", after.Exit, after.Stderr)
	}
	if before.Stdout != after.Stdout {
		t.Fatalf("help changed after slice value run:\n%s\n---\n%s", before.Stdout, after.Stdout)
	}
	if !strings.Contains(after.Stdout, "(default [x,y])") {
		t.Fatalf("help missing registered slice default:\n%s", after.Stdout)
	}
}

func TestCompletionRunResetsSliceFlags(t *testing.T) {
	app, echo := newSliceFixtureApp([]string{"arr-default"}, []string{"slice-default"})

	runSliceJSON(t, app, "leaf", "--l-arr", "stale", "--l-slice", "stale")
	res := testutil.Run(app, "__complete", "leaf", "")
	if res.Exit != rungrad.ExitSuccess {
		t.Fatalf("completion exit %d stderr=%q stdout=%q", res.Exit, res.Stderr, res.Stdout)
	}
	if !echo.completionSet {
		t.Fatal("completion did not call leaf ValidArgsFunction")
	}
	requireSlice(t, "completion l_arr", echo.completion.LArr, []string{"arr-default"})
	requireSlice(t, "completion l_slice", echo.completion.LSlice, []string{"slice-default"})
	requireSlice(t, "completion p_arr", echo.completion.PArr, []string{"arr-default"})
	requireSlice(t, "completion p_slice", echo.completion.PSlice, []string{"slice-default"})
}
