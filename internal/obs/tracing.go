// Package obs hosts process observability wiring shared by adapters.
//
// Tracing uses the OpenTelemetry global tracer with a stdout exporter (D9):
// every span carries the request correlationId, so logs, metrics and spans
// reconstruct the same flow. The global provider stays a no-op until
// SetupStdout runs, which keeps unit and integration tests silent; the Fx
// composition enables stdout in every deployed mode.
package obs

import (
	"context"
	"io"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// CorrelationAttr is the span attribute tying a span to its request logs.
const CorrelationAttr = "correlationId"

// SetupStdout installs the global tracer provider with a synchronous stdout
// exporter. The simple span processor exports inline: no background
// goroutine outlives the process, and Shutdown only flushes. It returns the
// shutdown function for the Fx lifecycle.
func SetupStdout(w io.Writer, service string) (func(context.Context) error, error) {
	exp, err := stdouttrace.New(stdouttrace.WithWriter(w))
	if err != nil {
		return nil, err
	}
	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceName(service)),
	)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns the shared tracer for wallet spans.
func Tracer() trace.Tracer {
	return otel.Tracer("github.com/jg-backend-challenge/wallet")
}

// Start opens a span carrying the correlation id. Callers end it with
// defer span.End().
func Start(ctx context.Context, name, correlationID string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	attrs = append([]attribute.KeyValue{attribute.String(CorrelationAttr, correlationID)}, attrs...)
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}
