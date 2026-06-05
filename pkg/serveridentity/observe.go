package serveridentity

import (
	"context"

	"github.com/go-logr/logr"
	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Instrumentation provides reusable logging and span annotations for token flows.
type Instrumentation struct {
	log     *logr.Logger
	span    trace.Span
	Verbose bool // useful for guarding expensive log calls
}

// NewInstrumentation creates an instrumentation instance from the context's logger and span.
func NewInstrumentation(ctx context.Context) Instrumentation {
	log := logger.FromContext(ctx)
	return Instrumentation{
		log:     log,
		span:    trace.SpanFromContext(ctx),
		Verbose: log.V(2).Enabled(),
	}
}

// LogDebug emits a V(2) structured log. Callers should guard with Verbose to avoid allocations.
func (o *Instrumentation) LogDebug(msg string, keysAndValues ...any) {
	o.log.V(2).Info(msg, keysAndValues...)
}

// AddEvent records a span event if the span is recording.
func (o *Instrumentation) AddEvent(name string, attrs ...attribute.KeyValue) {
	if o.span.IsRecording() {
		o.span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// RecordError records an error on the span and logs at Error level.
func (o *Instrumentation) RecordError(err error, attrs ...attribute.KeyValue) {
	if o.span.IsRecording() {
		o.span.RecordError(err, trace.WithAttributes(attrs...))
	}
	o.log.Error(err, "token fetch failed")
}

// ManagerHooks returns cache manager hooks wired to this instrumentation.
func (o *Instrumentation) ManagerHooks() *manager.Hooks {
	return &manager.Hooks{
		OnCacheHit: func(source string) {
			o.AddEvent("token.cache.hit", attribute.String("source", source))
			if o.Verbose {
				o.LogDebug("cache hit", "source", source)
			}
		},
		OnSuccess: func() {
			o.AddEvent("token.fetch.success")
		},
		OnFetchError: func(err error) {
			o.RecordError(err)
		},
	}
}

// spanError records an error on the span and sets its status to Error.
// If err is nil, only the status message is set.
func SpanError(span trace.Span, err error, msg string) {
	if !span.IsRecording() {
		return
	}
	if err != nil {
		span.RecordError(err)
	}
	span.SetStatus(codes.Error, msg)
}

// spanSetStatusCode sets the http.response.status_code attribute on the span.
func SpanSetStatusCode(span trace.Span, code int) {
	if span.IsRecording() {
		span.SetAttributes(attribute.Int("http.response.status_code", code))
	}
}
