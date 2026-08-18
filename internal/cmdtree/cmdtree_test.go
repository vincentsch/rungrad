package cmdtree

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestVisibleCommandsStableDepthFirst(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	alpha := &cobra.Command{Use: "alpha"}
	alpha.AddCommand(&cobra.Command{Use: "beta"}, &cobra.Command{Use: "alpha-child"})
	hidden := &cobra.Command{Use: "hidden", Hidden: true}
	help := &cobra.Command{Use: "help"}
	frameworkCompletion := &cobra.Command{
		Use:         "completion",
		Annotations: map[string]string{AnnotationFrameworkCompletion: "true"},
	}
	hostCompletion := &cobra.Command{Use: "completion"}
	hidden.AddCommand(&cobra.Command{Use: "hidden-child"})
	help.AddCommand(&cobra.Command{Use: "help-child"})
	frameworkCompletion.AddCommand(&cobra.Command{Use: "framework-completion-child"})
	hostCompletion.AddCommand(&cobra.Command{Use: "host-completion-child"})
	root.AddCommand(
		&cobra.Command{Use: "zeta"},
		alpha,
		hidden,
		help,
		frameworkCompletion,
		hostCompletion,
	)

	var got []string
	for _, cmd := range VisibleCommands(root) {
		got = append(got, cmd.Name())
	}
	want := []string{"root", "alpha", "alpha-child", "beta", "completion", "host-completion-child", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestGlobalFlagNames(t *testing.T) {
	root := &cobra.Command{Use: "root", Version: "v1"}
	root.PersistentFlags().String("config", "", "config path")
	root.PersistentFlags().Bool("json", false, "json output")
	child := &cobra.Command{Use: "child"}
	child.Flags().String("local", "", "local flag")
	root.AddCommand(child)

	got := GlobalFlagNames(root)
	for _, name := range []string{"help", "version", "config", "json"} {
		if !got[name] {
			t.Fatalf("missing global flag name %q in %v", name, got)
		}
	}
	if got["local"] {
		t.Fatalf("child local flag was treated as global: %v", got)
	}

	noVersion := &cobra.Command{Use: "root"}
	noVersion.PersistentFlags().Bool("json", false, "json output")
	got = GlobalFlagNames(noVersion)
	if got["version"] {
		t.Fatalf("version treated as global when root.Version is empty: %v", got)
	}
	if !got["help"] || !got["json"] {
		t.Fatalf("missing expected globals without version: %v", got)
	}
}
