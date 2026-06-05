package telemetry

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const providerShutdownTimeout = 5 * time.Second

func newProvider(cfg Config, agentVersion string) (*sdktrace.TracerProvider, error) {
	var exporters []sdktrace.SpanExporter

	if cfg.ConsoleExport {
		std, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("telemetry: stdout exporter: %w", err)
		}
		exporters = append(exporters, std)
	}

	endpoint, err := ResolveEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if endpoint != "" {
		opts := []otlptracehttp.Option{}
		if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
			opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
		} else {
			opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		}
		otlpExp, err := otlptracehttp.New(context.Background(), opts...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: otlp http exporter: %w", err)
		}
		exporters = append(exporters, otlpExp)
	}

	if len(exporters) == 0 {
		return nil, fmt.Errorf("telemetry: no exporters configured")
	}

	var batcher sdktrace.SpanExporter
	if len(exporters) == 1 {
		batcher = exporters[0]
	} else {
		batcher = newMultiExporter(exporters)
	}

	svc := cfg.ServiceName
	if svc == "" {
		svc = "agentic-control-plane"
	}
	ver := strings.TrimSpace(agentVersion)
	if ver == "" {
		ver = "unknown"
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(svc),
			semconv.ServiceVersion(ver),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(batcher),
		sdktrace.WithResource(res),
	)
	return tp, nil
}

type multiExporter struct {
	exporters []sdktrace.SpanExporter
}

func newMultiExporter(exporters []sdktrace.SpanExporter) sdktrace.SpanExporter {
	return &multiExporter{exporters: exporters}
}

func (m *multiExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	var first error
	for _, e := range m.exporters {
		if err := e.ExportSpans(ctx, spans); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *multiExporter) Shutdown(ctx context.Context) error {
	var first error
	for _, e := range m.exporters {
		if err := e.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func flushProvider(tp *sdktrace.TracerProvider) {
	if tp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerShutdownTimeout)
	defer cancel()
	if err := tp.Shutdown(ctx); err != nil {
		log.Printf("telemetry: shutdown: %v", err)
	}
}
