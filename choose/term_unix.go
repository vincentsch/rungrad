//go:build unix

package choose

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// discardPendingInput drops bytes already waiting on the terminal, such as
// keys typed or text pasted before the question appeared.
func discardPendingInput(fd int) {
	if err := unix.SetNonblock(fd, true); err != nil {
		return
	}
	defer unix.SetNonblock(fd, false)
	buf := make([]byte, 256)
	for i := 0; i < 64; i++ {
		n, err := unix.Read(fd, buf)
		if n <= 0 || err != nil {
			return
		}
	}
}

// restoreOnSignal puts the terminal back before the process dies from a
// termination signal while the menu is in raw mode, then lets the signal take
// its normal course. Ctrl-C does not raise SIGINT in raw mode; it arrives as a
// key and cancels the question.
func restoreOnSignal(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGINT)
	raise := func(sig os.Signal) {
		restore()
		signal.Reset(sig)
		if s, ok := sig.(syscall.Signal); ok {
			_ = syscall.Kill(os.Getpid(), s)
		}
	}
	go func() {
		select {
		case sig := <-ch:
			raise(sig)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
		// A signal that arrived while stopping must not be lost.
		select {
		case sig := <-ch:
			raise(sig)
		default:
		}
	}
}
