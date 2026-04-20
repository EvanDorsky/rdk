//go:build !windows

package logging

import "io"

// RegisterEventLogger does nothing on Unix. On Windows it will add an `Appender` for logging to
// windows event system. The returned io.Closer is a no-op on Unix; on Windows it flushes and
// tears down the background drain worker.
func RegisterEventLogger(_ Logger, _ string) io.Closer {
	return noopCloser{}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
