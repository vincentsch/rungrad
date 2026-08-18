package browser

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCommandFor(t *testing.T) {
	rawURL := `https://example.com/login?x=1;rm -rf /`
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{rawURL}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}},
		{"linux", "xdg-open", []string{rawURL}},
		{"plan9", "xdg-open", []string{rawURL}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := commandFor(tt.goos, rawURL)
			if name != tt.name || !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("commandFor() = %q %#v, want %q %#v", name, args, tt.name, tt.args)
			}
		})
	}
}

func TestLoginFlowRunSuccess(t *testing.T) {
	var opened []string
	polls := 0
	flow := LoginFlow{
		AuthURL:  "https://example.com/auth",
		Interval: time.Second,
		Open: func(ctx context.Context, url string) error {
			opened = append(opened, url)
			return nil
		},
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	err := flow.Run(context.Background(), func(context.Context) (bool, error) {
		polls++
		return polls == 3, nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(opened, []string{"https://example.com/auth"}) {
		t.Fatalf("opened = %v", opened)
	}
	if polls != 3 {
		t.Fatalf("polls = %d, want 3", polls)
	}
}

func TestLoginFlowInvalidInputsDoNotOpen(t *testing.T) {
	tests := []struct {
		name string
		flow LoginFlow
		poll func(context.Context) (bool, error)
	}{
		{"empty url", LoginFlow{Interval: time.Second}, func(context.Context) (bool, error) { return true, nil }},
		{"non-positive interval", LoginFlow{AuthURL: "https://example.com"}, func(context.Context) (bool, error) { return true, nil }},
		{"nil poll", LoginFlow{AuthURL: "https://example.com", Interval: time.Second}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opened := false
			tt.flow.Open = func(context.Context, string) error {
				opened = true
				return nil
			}
			if err := tt.flow.Run(context.Background(), tt.poll); err == nil {
				t.Fatal("Run() error = nil, want error")
			}
			if opened {
				t.Fatal("browser opened for invalid input")
			}
		})
	}
}

func TestLoginFlowPollError(t *testing.T) {
	want := errors.New("poll failed")
	polls := 0
	flow := LoginFlow{
		AuthURL:  "https://example.com/auth",
		Interval: time.Second,
		Open:     func(context.Context, string) error { return nil },
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}
	err := flow.Run(context.Background(), func(context.Context) (bool, error) {
		polls++
		return false, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if polls != 1 {
		t.Fatalf("polls = %d, want 1", polls)
	}
}

func TestLoginFlowOpenError(t *testing.T) {
	want := errors.New("open failed")
	polled := false
	flow := LoginFlow{
		AuthURL:  "https://example.com/auth",
		Interval: time.Second,
		Open:     func(context.Context, string) error { return want },
	}
	err := flow.Run(context.Background(), func(context.Context) (bool, error) {
		polled = true
		return true, nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if polled {
		t.Fatal("poll called after opener error")
	}
}

func TestLoginFlowSleepCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	want := context.Canceled
	polls := 0
	flow := LoginFlow{
		AuthURL:  "https://example.com/auth",
		Interval: time.Second,
		Open:     func(context.Context, string) error { return nil },
		Sleep: func(context.Context, time.Duration) error {
			cancel()
			return ctx.Err()
		},
	}
	err := flow.Run(ctx, func(context.Context) (bool, error) {
		polls++
		return false, nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if polls != 1 {
		t.Fatalf("polls = %d, want 1", polls)
	}
}

func TestLoginFlowCanceledContextStopsBeforeOpenOrPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	polled := false
	flow := LoginFlow{
		AuthURL:  "https://example.com/auth",
		Interval: time.Second,
		Open: func(context.Context, string) error {
			opened = true
			return nil
		},
	}
	err := flow.Run(ctx, func(context.Context) (bool, error) {
		polled = true
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if opened {
		t.Fatal("browser opened after context cancellation")
	}
	if polled {
		t.Fatal("poll called after context cancellation")
	}
}
