//go:build windows

package logging

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
	"golang.org/x/sys/windows/svc/eventlog"
)

var (
	eventLogMaxQueueSize = 20000
	eventLogDrainInterval = 100 * time.Millisecond
)

// RegisterEventLogger does nothing on Unix. On Windows it adds an Appender that
// buffers entries in memory and drains them to the Windows Event Log from a
// background goroutine. The returned io.Closer stops the background worker and
// flushes any remaining entries; callers should defer Close() so buffered logs
// reach the Event Log before the process exits.
func RegisterEventLogger(rootLogger Logger, name string) io.Closer {
	log, err := eventlog.Open(name)
	if err != nil {
		rootLogger.Errorw("Unable to open windows event log", "err", err)
		return noopCloser{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	el := &eventLogger{
		log:       log,
		cancelCtx: ctx,
		cancel:    cancel,
	}
	el.workers.Add(1)
	go el.backgroundWorker()

	rootLogger.AddAppender(el)
	return el
}

type queuedEntry struct {
	level zapcore.Level
	msg   string
}

type eventLogger struct {
	log *eventlog.Log

	mu                  sync.Mutex
	queue               []queuedEntry
	overflowsSinceDrain int

	// syncMu serializes drains so we don't issue concurrent Event Log writes
	// for overlapping ranges of the queue.
	syncMu sync.Mutex

	cancelCtx context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
	closeOnce sync.Once
}

func getMessage(entry zapcore.Entry, fields []zapcore.Field) string {
	const maxLength = 10
	toPrint := make([]string, 0, maxLength)
	// We use UTC such that logs from different `viam-server`s can have their logs compared without
	// needing them to be configured in the same timezone.
	toPrint = append(toPrint, entry.Time.UTC().Format(DefaultTimeFormatStr))
	toPrint = append(toPrint, strings.ToUpper(entry.Level.String()))
	toPrint = append(toPrint, entry.LoggerName)
	if entry.Caller.Defined {
		toPrint = append(toPrint, callerToString(&entry.Caller))
	}
	toPrint = append(toPrint, entry.Message)
	if len(fields) == 0 {
		return strings.Join(toPrint, "\t")
	}

	fieldsJSON, err := ZapcoreFieldsToJSON(fields)
	if err != nil {
		return strings.Join(toPrint, "\t")
	}
	toPrint = append(toPrint, fieldsJSON)

	return strings.Join(toPrint, "\t")
}

// Write formats the entry and enqueues it. On Fatal/Panic/DPanic levels the
// queue is drained synchronously before returning so logs reach the Event Log
// before zap tears the process down.
func (el *eventLogger) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// null characters cause golang to panic while trying to convert this to a windows string.
	msg := strings.ReplaceAll(getMessage(entry, fields), "\x00", "NUL")

	el.mu.Lock()
	if len(el.queue) >= eventLogMaxQueueSize {
		el.queue = el.queue[1:]
		el.overflowsSinceDrain++
	}
	el.queue = append(el.queue, queuedEntry{level: entry.Level, msg: msg})
	el.mu.Unlock()

	switch entry.Level {
	case zapcore.FatalLevel, zapcore.DPanicLevel, zapcore.PanicLevel:
		// program is going to go away; try to get everything out first.
		el.drain()
	}
	return nil
}

// Sync flushes all currently-queued entries to the Event Log.
func (el *eventLogger) Sync() error {
	el.drain()
	return nil
}

// Close stops the background worker and flushes remaining entries.
func (el *eventLogger) Close() error {
	el.closeOnce.Do(func() {
		el.cancel()
		el.workers.Wait()
		el.drain()
		// best-effort; the eventlog handle has no meaningful error to surface here.
		_ = el.log.Close()
	})
	return nil
}

func (el *eventLogger) backgroundWorker() {
	defer el.workers.Done()
	ticker := time.NewTicker(eventLogDrainInterval)
	defer ticker.Stop()
	for {
		select {
		case <-el.cancelCtx.Done():
			return
		case <-ticker.C:
			el.drain()
		}
	}
}

// drain swaps the queue out under the mutex, then writes each entry to the
// Event Log without holding the queue mutex. syncMu serializes concurrent
// drain callers (background worker vs. Sync/Write-on-fatal vs. Close).
func (el *eventLogger) drain() {
	el.syncMu.Lock()
	defer el.syncMu.Unlock()

	el.mu.Lock()
	batch := el.queue
	overflows := el.overflowsSinceDrain
	el.queue = nil
	el.overflowsSinceDrain = 0
	el.mu.Unlock()

	for _, qe := range batch {
		el.writeOne(qe)
	}

	if overflows > 0 {
		el.writeOne(queuedEntry{
			level: zapcore.WarnLevel,
			msg: strings.Join([]string{
				time.Now().UTC().Format(DefaultTimeFormatStr),
				"WARN",
				"event-logger",
				"event log buffer overflowed; dropped oldest entries",
			}, "\t"),
		})
	}
}

func (el *eventLogger) writeOne(qe queuedEntry) {
	switch qe.level {
	case zapcore.DebugLevel, zapcore.InfoLevel:
		_ = el.log.Info(0, qe.msg)
	case zapcore.WarnLevel:
		_ = el.log.Warning(0, qe.msg)
	default: // includes zapcore.ErrorLevel and "more threatening" levels
		_ = el.log.Error(0, qe.msg)
	}
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
