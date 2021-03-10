package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptrace"
	"os"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/google/uuid"

	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
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
	"go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"
)

const (
	appName     = "otel-demo"
	serviceName = "frontend"
	meterName   = "myMeter"
)

var (
	commonLabels = []attribute.KeyValue{
		attribute.String("app", appName),
		attribute.String("svc", serviceName)}
	reqCounter metric.Float64Counter
	errCounter metric.Float64Counter
	logger log.Logger
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

	return exporter
}

func mainHandler(w http.ResponseWriter, req *http.Request) {
	// Measure duration
	start := time.Now()

	// Standard header
	w.Header().Add("Content-Type", "text/html")

	// Read cookie
	var sessionId string
	session, err := req.Cookie("session_id")
	if err != nil {
		// Set cookie if non found
		session = &http.Cookie{
			Name: "session_id",
			Value: uuid.New().String(),
		}
		level.Info(logger).Log(
			"msg", "new session created",
			"sessionId", session.Value)
		http.SetCookie(w, session)
	}
	sessionId = session.Value

	// Pass the session_id via context
	ctx := baggage.ContextWithValues(
		req.Context(),
		attribute.String("session_id", sessionId))

	// Inject W3C context
	_, req = otelhttptrace.W3C(ctx, req)
	otelhttptrace.Inject(ctx, req)

	// Record request metric
	reqCounter.Add(ctx, float64(1), commonLabels...)

	// Call the backend service
	backendEndpoint := os.Getenv("BACKEND_ENDPOINT")

	if backendEndpoint == "" {
		backendEndpoint = "http://localhost:8888/api/main"
	}

	// New Tracer
	tr := otel.Tracer("main-handler")

	// New HTTP client for the backend
	client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}

	var body []byte

	err = func(ctx context.Context) error {
		// New span
		ctx, span := tr.Start(
			ctx, "main-handler",
			trace.WithAttributes(semconv.PeerServiceKey.String("backend")))
		defer span.End()

		ctx = httptrace.WithClientTrace(ctx, otelhttptrace.NewClientTrace(ctx))
		req, _ := http.NewRequestWithContext(ctx, "GET", backendEndpoint, nil)
		res, err := client.Do(req)

		duration := time.Since(start)

		if err == nil {
			body, err = ioutil.ReadAll(res.Body)
			_ = res.Body.Close()

			level.Info(logger).Log(
				"sessionId", sessionId,
				"traceID", span.SpanContext().TraceID,
				"spanID", span.SpanContext().SpanID,
				"duration", duration)
		} else {
			level.Error(logger).Log(
				"sessionId", sessionId,
				"traceID", span.SpanContext().TraceID,
				"spanID", span.SpanContext().SpanID,
				"duration", duration,
				"msg", "backend connection error",
				"err", err)

			// Show error in the span
			span.AddEvent(
				"backend connection error",
				trace.WithAttributes(
					attribute.Key("error").String(err.Error())))

			// Record error metric
			errCounter.Add(ctx, float64(1), commonLabels...)
		}

		return err
	}(ctx)

	if err != nil {
		// HTML output
		fmt.Fprintf(w, "Hello world from the %s\n", serviceName)
	} else {
		fmt.Fprint(w, string(body))
	}
}

func main() {
	// Init logger
	initLogger()

	// Init tracer
	flush := initTracer()
	defer flush()

	// Init meter
	meter := initMeter()

	// Setup HTTP server
	listen := os.Getenv("FRONTEND_LISTEN")

	if listen == "" {
		listen = "localhost:8080"
	}

	level.Info(logger).Log(
		"msg", fmt.Sprintf("%s listening on %s", strings.Title(serviceName), listen))

	http.HandleFunc("/", mainHandler)
	http.HandleFunc("/metrics", meter.ServeHTTP)

	err := http.ListenAndServe(listen, nil)
	if err != nil {
		level.Error(logger).Log(
			"msg", "cannot create HTTP server",
			"err", err)
		os.Exit(1)
	}
}
