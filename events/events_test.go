package events

import (
	"testing"

	"go.uber.org/zap/zapcore"
	"go.viam.com/test"

	"go.viam.com/rdk/logging"
)

func TestLog(t *testing.T) {
	parent, observedLogs := logging.NewObservedTestLogger(t)
	eventsLogger := NewLogger(parent)

	eventsLogger.Log("server_start", "server", map[string]any{"pid": 42})

	entries := observedLogs.All()
	test.That(t, len(entries), test.ShouldEqual, 1)

	entry := entries[0]
	test.That(t, entry.Message, test.ShouldEqual, "server_start")
	test.That(t, entry.Level, test.ShouldEqual, zapcore.ErrorLevel)

	fields := entry.ContextMap()
	test.That(t, fields[UnitKey], test.ShouldEqual, "server")
	info, ok := fields[InfoKey].(map[string]any)
	test.That(t, ok, test.ShouldBeTrue)
	test.That(t, info["pid"], test.ShouldEqual, 42)
}
