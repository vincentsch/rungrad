//go:build !unix

package choose

// Raw mode is only used on Unix terminals; elsewhere these are no-ops.
func discardPendingInput(int) {}

func byteWaiting(int) func() bool { return nil }

func restoreOnSignal(func()) (stop func()) { return func() {} }
