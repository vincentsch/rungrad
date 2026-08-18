// Package browser provides small browser-opening helpers and a generic
// open-then-poll login skeleton. Product-specific login protocols stay in the
// adopting CLI.
package browser

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"
)

// Open launches the user's default browser at rawURL. The URL is passed as a
// single argument, never through a shell.
func Open(ctx context.Context, rawURL string) error {
	name, args := commandFor(runtime.GOOS, rawURL)
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// commandFor returns the per-platform browser handoff command.
func commandFor(goos, rawURL string) (name string, args []string) {
	switch goos {
	case "darwin":
		return "open", []string{rawURL}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		return "xdg-open", []string{rawURL}
	}
}

// Opener opens a URL in a browser.
type Opener func(ctx context.Context, url string) error

// LoginFlow drives a browser-based login by opening AuthURL once, then polling
// until done, an error, or context cancellation.
type LoginFlow struct {
	AuthURL  string
	Interval time.Duration
	Open     Opener
	Sleep    func(context.Context, time.Duration) error
}

// Run opens the browser, then invokes poll until it reports completion.
func (lf LoginFlow) Run(ctx context.Context, poll func(context.Context) (done bool, err error)) error {
	if lf.AuthURL == "" {
		return errors.New("auth URL is required")
	}
	if lf.Interval <= 0 {
		return errors.New("login poll interval must be positive")
	}
	if poll == nil {
		return errors.New("login poll function is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	open := lf.Open
	if open == nil {
		open = Open
	}
	if err := open(ctx, lf.AuthURL); err != nil {
		return err
	}
	sleep := lf.Sleep
	if sleep == nil {
		sleep = ctxSleep
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := poll(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if err := sleep(ctx, lf.Interval); err != nil {
			return err
		}
	}
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
