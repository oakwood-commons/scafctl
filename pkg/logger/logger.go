package logger

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Define an unexported custom type for the context key to prevent collisions.
type loggerContextKey struct{}

const (
	RootCommandKey = "root_command"
	SubCommandKey  = "sub_command"
	CommitKey      = "commit"
	VersionKey     = "version"
	BuildTimeKey   = "build_time"
	GoVersionKey   = "go_version"
	TimeStampKey   = "timestamp"
	MessageKey     = "message"
	// DurationKey    = "duration"
	// UrlKey         = "url"
	// EnvKey         = "environment"
)

var (
	once sync.Once // Ensures Setup is called only once

	// globalZapLogger is the underlying *zap.Logger for explicit Zap-specific operations like Sync().
	// It's package-private to prevent direct modification.
	globalZapLogger *zap.Logger

	// globalLogrLogger is the logr.Logger instance that application code will primarily use
	// if not retrieving from context, or as a default for context.
	// It's package-private to prevent direct modification.
	globalLogrLogger *logr.Logger

	// defaultNoopLogger is a logger that does nothing, used as a fallback.
	defaultNoopLogger logr.Logger = logr.Discard()
)

// LogFormat represents the output format for logs.
type LogFormat string

const (
	// FormatJSON outputs logs in JSON format.
	FormatJSON LogFormat = "json"
	// FormatText outputs logs in human-readable text format.
	FormatText LogFormat = "text"
)

// Options configures the logger behavior.
type Options struct {
	// Level is the minimum log level (-1=Debug, 0=Info, 1=Warn, 2=Error).
	Level int8
	// Format is the output format (json or text).
	Format LogFormat
	// Timestamps controls whether timestamps are included in log output.
	Timestamps bool
}

// DefaultOptions returns the default logger options.
func DefaultOptions() Options {
	return Options{
		Level:      0,
		Format:     FormatJSON,
		Timestamps: true,
	}
}

// Get initializes the global Zap and Logr loggers with default options.
// It can only be called once. Subsequent calls will have no effect.
// logLevel: The minimum logging level (-1=Debug, 0=Info, 1=Warn, 2=Error).
// This function must be called before using FromContext or any logging operations.
func Get(logLevel int8) *logr.Logger {
	return GetWithOptions(Options{
		Level:      logLevel,
		Format:     FormatJSON,
		Timestamps: true,
	})
}

// GetWithOptions initializes the global Zap and Logr loggers with custom options.
// It can only be called once. Subsequent calls will have no effect.
// This function must be called before using FromContext or any logging operations.
func GetWithOptions(opts Options) *logr.Logger {
	once.Do(func() {
		// Encoder Configuration: How log entries are formatted
		encoderCfg := zap.NewProductionEncoderConfig()
		encoderCfg.MessageKey = MessageKey

		// Configure timestamps
		if opts.Timestamps {
			encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
			encoderCfg.TimeKey = TimeStampKey
		} else {
			encoderCfg.TimeKey = "" // Disable timestamps
		}

		// Determine the minimum log level
		minimumLogLevel := zapcore.Level(opts.Level)

		buildInfo, _ := debug.ReadBuildInfo()

		// Create encoder based on format
		var encoder zapcore.Encoder
		if opts.Format == FormatText {
			encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
			encoder = zapcore.NewConsoleEncoder(encoderCfg)
		} else {
			encoder = zapcore.NewJSONEncoder(encoderCfg)
		}

		// Create a Zap Core: Combines encoder, sink (output destination), and level
		core := zapcore.NewCore(
			encoder,
			zapcore.Lock(os.Stderr),               // Output to standard error, safely (thread-safe)
			zap.NewAtomicLevelAt(minimumLogLevel), // Set the logging level
		).With(
			[]zapcore.Field{
				zap.String(CommitKey, settings.VersionInformation.Commit),
				zap.String(VersionKey, settings.VersionInformation.BuildVersion),
				zap.String(BuildTimeKey, settings.VersionInformation.BuildTime),
				zap.String(GoVersionKey, buildInfo.GoVersion),
			},
		)

		// Build the Zap logger with options
		// zap.AddCaller(): Includes file and line number where the log was called.
		// zap.AddStacktrace(zap.ErrorLevel): Captures stack traces for logs at Error level and above.
		// zap.WithFatalHook(zapcore.WriteThenPanic): Ensures logs are flushed before panicking on Fatal.
		globalZapLogger = zap.New(core,
			zap.AddCaller(),
			zap.AddStacktrace(zap.ErrorLevel),
			zap.WithFatalHook(zapcore.WriteThenPanic),
		)

		// Wrap the Zap logger with zapr to get a logr.Logger
		gl := zapr.NewLogger(globalZapLogger)
		globalLogrLogger = &gl
	})
	if globalLogrLogger == nil {
		// This should never happen due to once.Do, but just in case
		return &defaultNoopLogger
	}
	return globalLogrLogger
}

// WithLogger returns a new context with the provided logr.Logger attached.
// If the context already contains the same logger instance, it returns the original context.
// This allows logger propagation through context for structured logging.
func WithLogger(ctx context.Context, log *logr.Logger) context.Context {
	if lp, ok := ctx.Value(loggerContextKey{}).(*logr.Logger); ok {
		if lp == log {
			return ctx
		}
	}
	return context.WithValue(ctx, loggerContextKey{}, log)
}

// FromContext retrieves the logr.Logger from the context.
// If no logger is found in the context, it returns the globally configured logger.
// If Setup has not been called, it returns a no-op logger to prevent panics.
func FromContext(ctx context.Context) *logr.Logger {
	if log, ok := ctx.Value(loggerContextKey{}).(*logr.Logger); ok {
		return log
	} else if log := globalLogrLogger; log != nil {
		// If no logger in context, return the global logger.
		return log
	}
	// Fallback to a no-op logger if Setup hasn't been called at all.
	return &defaultNoopLogger
}

// Sync flushes any buffered log entries to their destination.
// This should be called before the application exits, typically via `defer logger.Sync()` in main.
func Sync() {
	if globalZapLogger != nil {
		if err := globalZapLogger.Sync(); err != nil {
			// Sync can return an error (e.g., if stderr is closed).
			// In most CLI cases, this is safe to ignore or log to a fallback.
			// Since this is Sync, we can't log to globalZapLogger itself.
			fmt.Fprintf(os.Stderr, "WARNING: failed to sync zap logger: %v\n", err)
		}
	}
}

// GetGlobalLogger returns the globally configured logr.Logger.
// This is useful for top-level logging in main where context might not be readily available,
// or as a fallback. It will return a no-op logger if Setup has not been called.
func GetGlobalLogger() *logr.Logger {
	if globalLogrLogger != nil {
		return globalLogrLogger
	}
	return &defaultNoopLogger
}

func GetNoopLogger() *logr.Logger {
	return &defaultNoopLogger
}

// WithValues returns a new logr.Logger with additional key-value pairs for structured logging.
// The provided keysAndValues are added to the logger's context, allowing for richer log output.
// lgr: The base logger to augment.
// keysAndValues: Variadic list of key-value pairs to associate with the logger.
// Returns a pointer to the new logger with the added values.
func WithValues(lgr *logr.Logger, keysAndValues ...any) *logr.Logger {
	nlgr := lgr.WithValues(keysAndValues...)
	return &nlgr
}
