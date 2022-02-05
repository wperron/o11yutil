// Copyright 2021 William Perron. All rights reserved. MIT License.
package client

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/wperron/o11yutil/config"
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
	Ping(config.Target, chan<- Result, chan<- error)
}

type pinger struct {
	client *http.Client
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
	// do something
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

func NewInstrumentedPinger(target string) *pinger {
	client := &http.Client{}
	*client = *http.DefaultClient
	client.Timeout = 1 * time.Second

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
	}
}

func (p *pinger) Ping(t config.Target, out chan<- Result, e chan<- error) {
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

		start := time.Now()
		res, err := p.client.Do(&req)
		lat := time.Since(start)
		if err != nil {
			e <- fmt.Errorf("error: %s", err)
		} else {
			// Reading and closing the body is important to ensure that the file
			// descriptor is not leaked.
			_, _ = ioutil.ReadAll(res.Body)
			_ = res.Body.Close()
			out <- Result{
				Name:       t.Name,
				Method:     "GET",
				Status:     res.StatusCode,
				StatusText: res.Status,
				URL:        t.Url,
				Latency:    int(lat.Milliseconds()),
				TraceID:    res.Header.Get(t.TraceHeader),
			}
		}
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
