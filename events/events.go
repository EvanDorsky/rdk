// Package events records robot lifecycle events as specially-structured logs.
package events

import "go.viam.com/rdk/logging"

// LoggerName identifies event log entries; sublogger names compose,
// so the full name is e.g. "rdk.events".
const LoggerName = "events"

// Reserved structured-field keys, fixed so the app can parse events.
const (
	// UnitKey is the structured-field key holding the unit an event pertains to.
	UnitKey = "unit"
	// InfoKey is the structured-field key holding an event's extra information.
	InfoKey = "info"
)

// Logger records events.
type Logger struct {
	logger logging.Logger
}

// NewLogger derives the "events" sublogger from parent. Pass the robot's
// logger so event entries inherit its appenders and reach app.
func NewLogger(parent logging.Logger) *Logger {
	l := parent.Sublogger(LoggerName)
	l.NeverDeduplicate()
	return &Logger{logger: l}
}

// Log records a single event. Events log at ERROR level deliberately: ERROR
// is the highest log level, so no log-level configuration can filter events
// out. The event type is the log message; unit and info are structured
// fields, with info nested under a single key to avoid collisions with
// reserved keys.
func (l *Logger) Log(eventType, unit string, info map[string]any) {
	l.logger.Errorw(eventType, UnitKey, unit, InfoKey, info)
}
