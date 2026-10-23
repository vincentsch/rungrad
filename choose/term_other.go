//go:build !unix

package choose

// discardPendingInput does nothing here. Raw terminal input is only used on
// Unix.
func discardPendingInput(int) {}

// byteWaiting never reports pending input here. Raw terminal input is only
// used on Unix.
func byteWaiting(int) func() bool { return nil }

func restoreOnSignal(func()) (stop func()) { return func() {} }
