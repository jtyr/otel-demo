package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/trace/jaeger"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	appName     = "otel-demo"
	serviceName = "backend"
	meterName   = "myMeter"
)

var (
	commonLabels = []attribute.KeyValue{
		attribute.String("app", appName),
		attribute.String("svc", serviceName)}
	reqCounter metric.Float64Counter
)

// initTracer creates a new trace provider instance and registers it as global trace provider.
func initTracer() func() {
	jeagerEndpoint := os.Getenv("JAEGER_ENDPOINT")

	if jeagerEndpoint == "" {
		jeagerEndpoint = "http://localhost:14268/api/traces"
	}

	// Create and install Jaeger export pipeline.
	flush, err := jaeger.InstallNewPipeline(
		jaeger.WithCollectorEndpoint(jeagerEndpoint),
		jaeger.WithProcess(jaeger.Process{
			ServiceName: serviceName,
			Tags: []attribute.KeyValue{
				attribute.String("exporter", "jaeger"),
			},
		}),
		jaeger.WithSDK(&sdktrace.Config{DefaultSampler: sdktrace.AlwaysSample()}),
	)
	if err != nil {
		log.Fatal(err)
	}

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{}))

	return flush
}

func initMeter() (*prometheus.Exporter) {
	exporter, err := prometheus.InstallNewPipeline(prometheus.Config{})
	if err != nil {
		log.Panicf("failed to initialize prometheus exporter %v", err)
	}

	meter := global.Meter(meterName)

	// Init the metrics
	reqCounter = metric.Must(meter).NewFloat64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of requests"))
	reqCounter.Add(context.Background(), float64(0), commonLabels...)

	return exporter
}

func mainHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// Get parameter from the context
	sId := attribute.Key("session_id")
	sessionId := baggage.Value(ctx, sId)

	// New span
	span := trace.SpanFromContext(ctx)

	// Add event
	span.AddEvent(
		"handling session_id",
		trace.WithAttributes(sId.String(sessionId.AsString())))

	log.Printf(
		"Handler: trace_id: %s; span_id=%s\n",
		span.SpanContext().TraceID,
		span.SpanContext().SpanID)
	log.Printf("Session ID: %s", sessionId.AsString())

	// Record request metric
	reqCounter.Add(ctx, float64(1), commonLabels...)

	// Send back a message
	fmt.Fprintf(w, "Hello world from the %s\n", serviceName)
}

func main() {
	flush := initTracer()
	defer flush()

	meter := initMeter()

	listen := os.Getenv("BACKEND_LISTEN")

	if listen == "" {
		listen = "localhost:8888"
	}

	log.Printf("%s listening on %s\n", strings.Title(serviceName), listen)

	otelHandler := otelhttp.NewHandler(
		http.HandlerFunc(mainHandler),
		"main-handler")

	http.Handle("/api/main", otelHandler)
	http.HandleFunc("/metrics", meter.ServeHTTP)

	err := http.ListenAndServe(listen, nil)
	if err != nil {
		panic(err)
	}
}
