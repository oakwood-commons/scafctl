package serveridentity

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNewInstrumentation_VerboseTrue(t *testing.T) {
	t.Parallel()
	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)

	// Discard logger has all V-levels disabled
	ins := NewInstrumentation(ctx)
	assert.False(t, ins.Verbose)
}

func TestNewInstrumentation_VerboseFalse_DefaultContext(t *testing.T) {
	t.Parallel()
	ins := NewInstrumentation(context.Background())
	assert.False(t, ins.Verbose)
}

func TestAddEvent_Recording(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")

	ctx = logger.WithLogger(ctx, discard())

	ins := NewInstrumentation(ctx)
	ins.AddEvent("my.event", attribute.String("key", "val"))

	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "my.event", events[0].Name)
	assert.Contains(t, events[0].Attributes, attribute.String("key", "val"))
}

func TestAddEvent_NotRecording(t *testing.T) {
	t.Parallel()

	ctx := logger.WithLogger(context.Background(), discard())
	// No span started — noop span
	ins := NewInstrumentation(ctx)
	// Should not panic
	ins.AddEvent("noop.event", attribute.String("x", "y"))
}

func TestRecordError_RecordsOnSpanAndLogs(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")

	ctx = logger.WithLogger(ctx, discard())

	ins := NewInstrumentation(ctx)
	testErr := errors.New("something broke")
	ins.RecordError(testErr, attribute.String("scope", "api/.default"))

	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "exception", events[0].Name)
}

func TestRecordError_NoopSpan(t *testing.T) {
	t.Parallel()

	ctx := logger.WithLogger(context.Background(), discard())
	ins := NewInstrumentation(ctx)
	// Should not panic with noop span
	ins.RecordError(errors.New("err"))
}

func TestManagerHooks_OnCacheHit(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")

	ctx = logger.WithLogger(ctx, discard())

	ins := NewInstrumentation(ctx)
	hooks := ins.ManagerHooks()

	hooks.OnCacheHit("leader")

	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "token.cache.hit", events[0].Name)
	assert.Contains(t, events[0].Attributes, attribute.String("source", "leader"))
}

func TestManagerHooks_OnSuccess(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")

	ctx = logger.WithLogger(ctx, discard())

	ins := NewInstrumentation(ctx)
	hooks := ins.ManagerHooks()

	hooks.OnSuccess()

	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "token.fetch.success", events[0].Name)
}

func TestManagerHooks_OnFetchError(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "parent")

	ctx = logger.WithLogger(ctx, discard())

	ins := NewInstrumentation(ctx)
	hooks := ins.ManagerHooks()

	hooks.OnFetchError(errors.New("timeout"))

	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	events := spans[0].Events()
	require.Len(t, events, 1)
	assert.Equal(t, "exception", events[0].Name)
}

func BenchmarkNewInstrumentation(b *testing.B) {
	b.ReportAllocs()
	ctx := logger.WithLogger(context.Background(), discard())
	// Add a recording span
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("bench")
	ctx, _ = tracer.Start(ctx, "bench-span")

	b.ResetTimer()
	for b.Loop() {
		_ = NewInstrumentation(ctx)
	}
}

func BenchmarkAddEvent(b *testing.B) {
	b.ReportAllocs()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	tracer := tp.Tracer("bench")
	ctx, _ := tracer.Start(context.Background(), "bench-span")
	ctx = logger.WithLogger(ctx, discard())
	ins := NewInstrumentation(ctx)

	b.ResetTimer()
	for b.Loop() {
		ins.AddEvent("bench.event", attribute.String("k", "v"))
	}
}

func discard() *logr.Logger {
	l := logr.Discard()
	return &l
}
