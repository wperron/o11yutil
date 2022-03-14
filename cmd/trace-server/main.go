package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wperron/o11yutil/debugprocessor"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

var (
	addr          = flag.String("addr", "", "Address the api will listen on.")
	traceEndpoint = flag.String("trace", "", "Address for the OpenTelemetry Collector.")
	tracer        trace.Tracer
	latency       prometheus.Histogram
)

func main() {
	flag.Parse()

	// Setup tracing
	ctx := context.Background()
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(*traceEndpoint),
		// TODO(wperron) replace grpc.WithTimeout, deprecated
		otlptracegrpc.WithDialOption(grpc.WithBlock(), grpc.WithTimeout(5*time.Second)), // nolint
	)
	if err != nil {
		log.Fatalf("failed to create trace exporter: %s", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String("trace-server"),
		),
	)
	if err != nil {
		log.Fatalf("failed to create trace resource: %s", err)
	}

	// Test debug span processor
	debug := debugprocessor.New().WithWriter(os.Stdout).Build()

	bsp := sdktrace.NewBatchSpanProcessor(exp)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(debug),
		sdktrace.WithSpanProcessor(bsp),
	)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(tracerProvider)

	defer tracerProvider.Shutdown(ctx) // nolint

	tracer = otel.Tracer("trace-server")

	// Create and register basic prometheus metrics for the API's usage
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "A counter for requests to the api.",
		},
		[]string{"code", "method"},
	)

	latency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "api_requests_latency",
			Help: "A histogram for api response latencies.",
		},
	)

	inFlight := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "api_requests_in_flight",
			Help: "A gauge for the number of in-flight requests.",
		},
	)

	prometheus.MustRegister(counter, latency, inFlight)

	http.Handle("/", promhttp.InstrumentHandlerCounter(
		counter, promhttp.InstrumentHandlerInFlight(inFlight, InstrumentedHandler(new(handler))),
	))

	http.Handle("/metrics", InstrumentedHandler(promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars
			EnableOpenMetrics: true,
		},
	)))

	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler")
	defer span.End()
	randomRecurse(ctx, 0, 10, int(200*time.Millisecond), int(1000*time.Millisecond))
	fmt.Fprint(w, "Hello, World!")
}

func randomRecurse(ctx context.Context, curr, max, minDur, maxDur int) {
	dur := time.Duration(rand.Intn(maxDur-minDur) + minDur)
	ctx, span := tracer.Start(ctx, "recurse", trace.WithAttributes(
		attribute.Int("duration", int(dur.Milliseconds())),
		attribute.Int("depth", curr),
	))
	defer span.End()

	time.Sleep(dur)
	if curr == max {
		return
	}

	if rand.Intn(2)&1 == 1 {
		curr++
		randomRecurse(ctx, curr, max, minDur, maxDur)
	}
}

func InstrumentedHandler(next http.Handler) http.Handler {
	handlerFunc := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		d := newDelegator(w)
		ctx := r.Context()
		traceID := trace.SpanContextFromContext(ctx).TraceID().String()
		next.ServeHTTP(d, r)
		latency.(prometheus.ExemplarObserver).ObserveWithExemplar(
			time.Since(start).Seconds(), prometheus.Labels{"traceID": traceID},
		)
		fmt.Printf("traceID=%s path=%s method=%s status=%d bytes=%d\n", traceID, r.URL.Path, r.Method, d.statusCode, d.written)
	}

	otelHandler := otelhttp.NewHandler(http.HandlerFunc(handlerFunc), "http")

	return otelHandler
}

type responseWriterDelegator struct {
	http.ResponseWriter
	statusCode  int
	written     int64
	wroteHeader bool
}

func (d *responseWriterDelegator) WriteHeader(statusCode int) {
	d.statusCode = statusCode
}

func (d *responseWriterDelegator) Write(b []byte) (int, error) {
	if !d.wroteHeader {
		d.WriteHeader(http.StatusOK)
	}
	n, err := d.ResponseWriter.Write(b)
	d.written += int64(n)
	return n, err
}

func (d *responseWriterDelegator) Flush() {
	if !d.wroteHeader {
		d.WriteHeader(http.StatusOK)
	}
	d.ResponseWriter.(http.Flusher).Flush()
}

func newDelegator(w http.ResponseWriter) *responseWriterDelegator {
	return &responseWriterDelegator{
		ResponseWriter: w,
	}
}
