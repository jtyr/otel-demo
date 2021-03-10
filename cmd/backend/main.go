package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.opentelemetry.io/otel/exporters/trace/jaeger"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
	errCounter metric.Float64Counter
	logger log.Logger
	errorGenerator time.Duration
	maxResponseDuration time.Duration = 5 * time.Millisecond
)

// initLogger creates new logger used throughout the application.
func initLogger() {
	logger = log.NewLogfmtLogger(os.Stderr)
	logger = log.With(
		logger,
		"ts", log.DefaultTimestampUTC,
		"app", appName,
		"service", serviceName)
}

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
		level.Error(logger).Log(
			"msg", "cannot create tracer",
			"err", err)
		os.Exit(1)
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
		level.Error(logger).Log(
			"msg", "failed to initialize Prometheus exporter",
			"err", err)
		os.Exit(1)
	}

	meter := global.Meter(meterName)

	ctx := context.Background()

	// Init the metrics
	reqCounter = metric.Must(meter).NewFloat64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of requests"))
	reqCounter.Add(ctx, float64(0), commonLabels...)

	errCounter = metric.Must(meter).NewFloat64Counter(
		"http_errors_total",
		metric.WithDescription("Total number of errors"))
	errCounter.Add(ctx, float64(0), commonLabels...)

	// Start collecting runtime metrics
	if err = runtime.Start(); err != nil {
		level.Error(logger).Log(
			"msg", "failed to initialize runtime metrics collection",
			"err", err)
		os.Exit(1)
	}

	return exporter
}

// mainHandler is the endpoint called from the Frontend.
func mainHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// Inject W3C context
	_, req = otelhttptrace.W3C(ctx, req)
	otelhttptrace.Inject(ctx, req)

	// Get parameter from the context
	sId := attribute.Key("session_id")
	sessionId := baggage.Value(ctx, sId)

	// New span
	span := trace.SpanFromContext(ctx)

	// Add event
	span.AddEvent(
		"handling session_id",
		trace.WithAttributes(sId.String(sessionId.AsString())))

	// Simulate slow response
	duration := time.Duration(rand.ExpFloat64()) * time.Millisecond
	time.Sleep(duration)

	if maxResponseDuration > 0 * time.Millisecond && duration > maxResponseDuration {
		msg := "response is taking too long"

		// Set the span status
		span.SetStatus(codes.Error, msg)

		// Record error metric
		errCounter.Add(ctx, float64(1), commonLabels...)

		level.Error(logger).Log(
			"sessionId", sessionId,
			"traceID", span.SpanContext().TraceID,
			"spanID", span.SpanContext().SpanID,
			"duration", duration,
			"msg", msg)

		// Return 408
		w.WriteHeader(http.StatusRequestTimeout)
	} else {
		level.Info(logger).Log(
			"sessionId", sessionId,
			"traceID", span.SpanContext().TraceID,
			"spanID", span.SpanContext().SpanID,
			"duration", duration)

		// Record request metric
		reqCounter.Add(ctx, float64(1), commonLabels...)

		// Send back a message
		fmt.Fprintf(w, "Hello world from the %s\n", serviceName)
	}
}

// errorGeneratorHandler is the endpoint called from the Frontend.
func errorGeneratorHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	if req.Method == "PUT" {
		body, err := ioutil.ReadAll(req.Body)

		if err != nil {
			// Record error metric
			errCounter.Add(ctx, float64(1), commonLabels...)

			level.Error(logger).Log(
				"msg", "failed to read request body from errorGeneratorHandler",
				"err", err)
		} else {
			value, cErr := strconv.ParseUint(string(body), 10, 8)
			if cErr != nil {
				// Record error metric
				errCounter.Add(ctx, float64(1), commonLabels...)

				level.Error(logger).Log(
					"msg", "failed to convert maxResponseDuration value",
					"err", cErr)
			} else {
				maxResponseDuration = time.Duration(value) * time.Millisecond

				level.Warn(logger).Log(
					"msg", fmt.Sprintf("errorGenerator set to value %d from host %s", value, req.RemoteAddr))
			}
		}
	}

	fmt.Fprintf(w, "maxResponseDuration=%v\n", maxResponseDuration)
}

func main() {
	// Init random generator
	rand.Seed(time.Now().UnixNano())

	// Init logger
	initLogger()

	// Setup tracer
	flush := initTracer()
	defer flush()

	// Setup meter
	meter := initMeter()

	// Setup HTTP server
	listen := os.Getenv("BACKEND_LISTEN")

	if listen == "" {
		listen = "localhost:8888"
	}

	level.Info(logger).Log(
		"msg", fmt.Sprintf("%s listening on %s", strings.Title(serviceName), listen))

	otelHandler := otelhttp.NewHandler(
		http.HandlerFunc(mainHandler),
		"main-handler")

	http.Handle("/api/main", otelHandler)
	http.HandleFunc("/api/features/errorGenerator", errorGeneratorHandler)
	http.HandleFunc("/metrics", meter.ServeHTTP)

	err := http.ListenAndServe(listen, nil)
	if err != nil {
		level.Error(logger).Log(
			"msg", "cannot create HTTP server",
			"err", err)
		os.Exit(1)
	}
}
