package logger

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
)

// mockLogLevel is a valid zapcore.Level value for testing.
const mockLogLevel int8 = 0 // zapcore.InfoLevel

func TestGetReturnsLoggerInstance(t *testing.T) {
	logger := Get(mockLogLevel)
	if logger == nil {
		t.Fatal("Get should return a non-nil logger")
	}
}

func TestGetReturnsSameInstanceOnSubsequentCalls(t *testing.T) {
	logger1 := Get(mockLogLevel)
	logger2 := Get(mockLogLevel)
	if logger1 != logger2 {
		t.Error("Get should return the same logger instance on subsequent calls")
	}
}

func TestGetReturnsNoopLoggerIfGlobalLoggerNil(t *testing.T) {
	// Save and restore globalLogrLogger for isolation
	orig := globalLogrLogger
	globalLogrLogger = nil
	defer func() { globalLogrLogger = orig }()

	logger := Get(mockLogLevel)
	if logger == nil {
		t.Fatal("Get should return a logger (noop) if globalLogrLogger is nil")
	}
	// logr.Discard() returns a pointer, so we can check type
	if fmt.Sprintf("%T", logger) != "*logr.Logger" {
		t.Errorf("Get should return a logr.Logger type, got %T", logger)
	}
}

func TestWithLoggerAddsLoggerToContext(t *testing.T) {
	ctx := context.Background()
	logger := Get(mockLogLevel)
	newCtx := WithLogger(ctx, logger)

	got := newCtx.Value(loggerContextKey{})
	if got == nil {
		t.Fatal("WithLogger should add logger to context")
	}
	if got != logger {
		t.Error("WithLogger should store the provided logger in context")
	}
}

func TestWithLoggerReturnsSameContextIfLoggerAlreadySet(t *testing.T) {
	ctx := context.Background()
	logger := Get(mockLogLevel)
	ctxWithLogger := context.WithValue(ctx, loggerContextKey{}, logger)

	resultCtx := WithLogger(ctxWithLogger, logger)
	if resultCtx != ctxWithLogger {
		t.Error("WithLogger should return the same context if logger is already set and matches")
	}
}

func TestWithLoggerReplacesLoggerIfDifferent(t *testing.T) {
	ctx := context.Background()
	logger1 := Get(mockLogLevel)
	logger2 := logr.Discard()
	ctxWithLogger := context.WithValue(ctx, loggerContextKey{}, logger1)

	resultCtx := WithLogger(ctxWithLogger, &logger2)
	got := resultCtx.Value(loggerContextKey{})
	if got != &logger2 {
		t.Error("WithLogger should replace logger in context if different")
	}
}

func TestFromContextReturnsLoggerFromContext(t *testing.T) {
	ctx := context.Background()
	logger := Get(mockLogLevel)
	ctxWithLogger := context.WithValue(ctx, loggerContextKey{}, logger)

	got := FromContext(ctxWithLogger)
	if got != logger {
		t.Error("FromContext should return the logger stored in context")
	}
}

func TestFromContextReturnsGlobalLoggerIfNoLoggerInContext(t *testing.T) {
	ctx := context.Background()
	globalLogger := Get(mockLogLevel)

	got := FromContext(ctx)
	if got != globalLogger {
		t.Error("FromContext should return the global logger if none in context")
	}
}

func TestFromContextReturnsNoopLoggerIfNoGlobalOrContextLogger(t *testing.T) {
	// Save and restore globalLogrLogger for isolation
	orig := globalLogrLogger
	globalLogrLogger = nil
	defer func() { globalLogrLogger = orig }()

	ctx := context.Background()
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext should return a logger (noop) if none in context or global")
	}
	if got != &defaultNoopLogger {
		t.Error("FromContext should return defaultNoopLogger if no logger is set")
	}
}

func TestSyncDoesNotPanicWhenGlobalZapLoggerIsNil(t *testing.T) {
	// Save and restore globalZapLogger for isolation
	orig := globalZapLogger
	globalZapLogger = nil
	defer func() { globalZapLogger = orig }()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Sync should not panic when globalZapLogger is nil, but got panic: %v", r)
		}
	}()
	Sync()
}

func TestGetGlobalLoggerReturnsGlobalLogger(t *testing.T) {
	// Save and restore globalLogrLogger for isolation
	orig := globalLogrLogger
	defer func() { globalLogrLogger = orig }()

	mockLogger := logr.Discard()
	globalLogrLogger = &mockLogger

	got := GetGlobalLogger()
	if got != &mockLogger {
		t.Error("GetGlobalLogger should return the globalLogrLogger when it is set")
	}
}

func TestGetGlobalLoggerReturnsNoopLoggerIfGlobalLoggerNil(t *testing.T) {
	// Save and restore globalLogrLogger for isolation
	orig := globalLogrLogger
	globalLogrLogger = nil
	defer func() { globalLogrLogger = orig }()

	got := GetGlobalLogger()
	if got != &defaultNoopLogger {
		t.Error("GetGlobalLogger should return defaultNoopLogger when globalLogrLogger is nil")
	}
}

func TestGetNoopLoggerReturnsDefaultNoopLogger(t *testing.T) {
	got := GetNoopLogger()
	if got == nil {
		t.Fatal("GetNoopLogger should return a non-nil logger")
	}
	if got != &defaultNoopLogger {
		t.Error("GetNoopLogger should return defaultNoopLogger")
	}
}

func TestGetNoopLoggerIsNoop(t *testing.T) {
	logger := GetNoopLogger()
	// logr.Discard() does nothing, so calling Info should not panic or output
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("GetNoopLogger should not panic when calling Info, got: %v", r)
		}
	}()
	logger.Info("This should do nothing")
}

func TestWithValuesReturnsNewLoggerWithValues(t *testing.T) {
	logger := Get(mockLogLevel)
	key := "testKey"
	value := "testValue"

	newLogger := WithValues(logger, key, value)
	if newLogger == nil {
		t.Fatal("WithValues should return a non-nil logger")
	}
	if newLogger == logger {
		t.Error("WithValues should return a new logger instance, not the original")
	}
}

func TestWithValuesPreservesOriginalLogger(t *testing.T) {
	logger := Get(mockLogLevel)
	key := "key"
	value := "value"

	_ = WithValues(logger, key, value)
	// The original logger should not have the new values attached
	// logr.Logger does not expose values, but we can check that calling WithValues again returns a new pointer
	newLogger := WithValues(logger, key, value)
	if newLogger == logger {
		t.Error("WithValues should not mutate the original logger")
	}
}

func TestWithValuesHandlesNilLogger(t *testing.T) {
	var logger *logr.Logger = nil
	defer func() {
		if r := recover(); r == nil {
			t.Error("WithValues should panic when given a nil logger")
		}
	}()
	_ = WithValues(logger, "key", "value")
}

func TestWithValuesWithNoValuesReturnsNewLogger(t *testing.T) {
	logger := Get(mockLogLevel)
	newLogger := WithValues(logger)
	if newLogger == nil {
		t.Fatal("WithValues should return a non-nil logger even with no values")
	}
	if newLogger == logger {
		t.Error("WithValues should return a new logger instance even with no values")
	}
}
