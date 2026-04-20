//go:build windows

package logging

import (
	"testing"

	"go.viam.com/test"
)

func TestWindowsNulls(t *testing.T) {
	logger := NewLogger("nulls")
	closer := RegisterEventLogger(logger, "viam-server")
	defer func() {
		test.That(t, closer.Close(), test.ShouldBeNil)
	}()
	logger.Info("this \x00 is a null")
	err := logger.Sync()
	test.That(t, err, test.ShouldBeNil)
}
