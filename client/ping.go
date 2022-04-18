// Copyright 2021 William Perron. All rights reserved. MIT License.
package client

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wperron/o11yutil/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	defaultDelay  = 10000 * time.Millisecond
	defaultJitter = 0.2
	DefaultPinger = &pinger{
		client: http.DefaultClient,
	}

	inFlightGauge  *prometheus.GaugeVec
	requestCounter *prometheus.CounterVec
	dnsLatencyVec  *prometheus.HistogramVec
	tlsLatencyVec  *prometheus.HistogramVec
	reqLatencyVec  *prometheus.HistogramVec
)

type Pinger interface {
	Ping(context.Context, config.Target)
}

type pinger struct {
	client *http.Client
	tracer trace.Tracer
}

type Result struct {
	Name       string
	Method     string
	Status     int
	StatusText string
	URL        string
	Latency    int
	TraceID    string
}

func init() {
	inFlightGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "client_in_flight_requests",
			Help: "A gauge of in-flight requests for the wrapped client.",
		},
		[]string{"target"},
	)

	requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "client_api_requests_total",
			Help: "A counter for requests from the wrapped client.",
		},
		[]string{"target", "code", "method"},
	)

	// dnsLatencyVec uses custom buckets based on expected dns durations.
	// It has an instance label "event", which is set in the
	// DNSStart and DNSDonehook functions defined in the
	// InstrumentTrace struct below.
	dnsLatencyVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dns_duration_seconds",
			Help:    "Trace dns latency histogram.",
			Buckets: []float64{.005, .01, .025, .05},
		},
		[]string{"target", "event"},
	)

	// tlsLatencyVec uses custom buckets based on expected tls durations.
	// It has an instance label "event", which is set in the
	// TLSHandshakeStart and TLSHandshakeDone hook functions defined in the
	// InstrumentTrace struct below.
	tlsLatencyVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tls_duration_seconds",
			Help:    "Trace tls latency histogram.",
			Buckets: []float64{.05, .1, .25, .5},
		},
		[]string{"target", "event"},
	)

	// reqLatencyVec has no labels, making it a zero-dimensional ObserverVec.
	reqLatencyVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "request_duration_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"target"},
	)

	// Register all of the metrics in the standard registry.
	prometheus.MustRegister(requestCounter, tlsLatencyVec, dnsLatencyVec, reqLatencyVec, inFlightGauge)
}

func NewInstrumentedPinger(target string, tracer trace.Tracer) *pinger {
	client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	client.Timeout = 10 * time.Second

	// Wrap the default RoundTripper with middleware.
	roundTripper := InstrumentRoundTripperInFlight(inFlightGauge, &target,
		InstrumentRoundTripperCounter(requestCounter, &target,
			InstrumentRoundTripperDuration(reqLatencyVec, &target, http.DefaultTransport),
		),
	)

	// Set the RoundTripper on our client.
	client.Transport = roundTripper
	return &pinger{
		client: client,
		tracer: tracer,
	}
}

func (p *pinger) Ping(ctx context.Context, t config.Target) {
	u, err := url.Parse(t.Url)
	if err != nil {
		log.Fatalf("unable to parse URL %s", t.Url)
	}

	req := http.Request{
		Method: "GET",
		URL:    u,
	}

	if t.Headers != nil && len(*t.Headers) > 0 {
		req.Header = *t.Headers
	}

	for {
		delay := float64(t.Duration())
		if delay == 0.0 {
			delay = float64(defaultDelay)
		}

		jitter := float64(t.Jitter)
		if jitter == 0.0 {
			jitter = float64(defaultJitter)
		}

		time.Sleep(Jitter(delay, jitter))

		currCtx, span := p.tracer.Start(ctx, "zombie.ping",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("target", u.Host),
			))

		// Overwrite the request context for the one containing the trace context
		req = *req.WithContext(currCtx)

		span.SetAttributes(semconv.HTTPClientAttributesFromHTTPRequest(&req)...)

		res, err := p.client.Do(&req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("client error: %s", err))
		} else {
			// Reading and closing the body is important to ensure that the file
			// descriptor is not leaked.
			_, _ = ioutil.ReadAll(res.Body)
			_ = res.Body.Close()

			span.SetAttributes(
				semconv.HTTPAttributesFromHTTPStatusCode(res.StatusCode)...,
			)
		}

		// Because this is an infinite loop, `defer` will only leak spans forever
		// hence the need to "manually" end each span
		span.End()
	}
}

type RoundTripperFunc func(req *http.Request) (*http.Response, error)

// RoundTrip implements the RoundTripper interface.
func (rt RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func InstrumentRoundTripperInFlight(gauge *prometheus.GaugeVec, target *string, next http.RoundTripper) RoundTripperFunc {
	return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		gauge.WithLabelValues(*target).Inc()
		defer gauge.WithLabelValues(*target).Dec()
		return next.RoundTrip(r)
	})
}

func InstrumentRoundTripperCounter(counter *prometheus.CounterVec, target *string, next http.RoundTripper) RoundTripperFunc {
	return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(r)
		if err == nil {

			counter.With(prometheus.Labels{
				"code":   fmt.Sprint(resp.StatusCode),
				"method": r.Method,
				"target": *target,
			}).Inc()
		}
		return resp, err
	})
}

func InstrumentRoundTripperDuration(obs prometheus.ObserverVec, target *string, next http.RoundTripper) RoundTripperFunc {
	return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		start := time.Now()
		resp, err := next.RoundTrip(r)
		if err == nil {
			obs.With(prometheus.Labels{
				"target": *target,
			}).Observe(time.Since(start).Seconds())
		}
		return resp, err
	})
}

func Jitter(val, jitter float64) (jittered time.Duration) {
	jittered = time.Duration(val * (1 + (jitter * (rand.Float64()*2 - 1))))
	return
}
