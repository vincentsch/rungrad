package testutil

import (
	"reflect"
	"strings"
	"testing"

	rungrad "github.com/vincentsch/rungrad"
)

func helpApp() *rungrad.App {
	app := rungrad.New(rungrad.AppConfig{Name: "rghelp", Short: "help test CLI", Version: "0.0.0"})
	app.Root().Example = "rghelp ping"
	ping := &rungrad.Command{
		Use:      "ping",
		Short:    "Ping a service",
		Examples: []string{"rghelp ping"},
	}
	ping.AddCommand(&rungrad.Command{
		Use:      "echo",
		Short:    "Echo a ping",
		Examples: []string{"rghelp ping echo"},
	})
	app.AddCommand(ping)
	return app
}

func TestCaptureHelp(t *testing.T) {
	root := CaptureHelp(helpApp())
	if root == "" || !strings.Contains(root, "rghelp ping") {
		t.Fatalf("root help missing example:\n%s", root)
	}
	sub := CaptureHelp(helpApp(), "ping")
	if sub == "" || !strings.Contains(sub, "rghelp ping") {
		t.Fatalf("ping help missing example:\n%s", sub)
	}
	if root2 := CaptureHelp(helpApp()); root2 != root {
		t.Fatalf("root help is not deterministic:\n%s\n---\n%s", root, root2)
	}
	if sub2 := CaptureHelp(helpApp(), "ping"); sub2 != sub {
		t.Fatalf("subcommand help is not deterministic:\n%s\n---\n%s", sub, sub2)
	}
}

func TestCaptureAllHelp(t *testing.T) {
	all := CaptureAllHelp(helpApp())
	wantKeys := []string{"", "ping", "ping echo"}
	gotKeys := sortedMapKeys(all)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("help keys = %v, want %v", gotKeys, wantKeys)
	}
	for _, key := range wantKeys {
		args := []string{}
		if key != "" {
			args = strings.Fields(key)
		}
		if got, want := all[key], CaptureHelp(helpApp(), args...); got != want {
			t.Fatalf("CaptureAllHelp(%q) differs from CaptureHelp:\n%s\n---\n%s", key, got, want)
		}
	}
}
