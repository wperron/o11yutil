package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr    = flag.String("addr", "", "The address to serve the api from.")
	counter *prometheus.CounterVec
	latency *prometheus.HistogramVec
)

func init() {
	counter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "A counter for requests from the wrapped client.",
		},
		[]string{"code", "method"},
	)

	latency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_requests_latency",
			Help:    "Trace dns latency histogram.",
			Buckets: []float64{.001, .005, .01, 0.025, .05, 0.1, 0.25, 0.5, 1, 5, 10},
		},
		[]string{"code", "method"},
	)

	prometheus.MustRegister(counter, latency)
}

func main() {
	flag.Parse()

	http.Handle("/", promhttp.InstrumentHandlerDuration(
		latency, promhttp.InstrumentHandlerCounter(counter, new(handler)),
	))

	metricsHandler := promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	)
	http.Handle("/metrics",
		promhttp.InstrumentHandlerDuration(
			latency, promhttp.InstrumentHandlerCounter(counter, metricsHandler),
		),
	)

	log.Fatal(http.ListenAndServe(*addr, nil))
}

type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	randomRecurse(0, 10, int(200*time.Millisecond), int(1000*time.Millisecond))
	fmt.Fprint(w, "Hello, World!")
}

func randomRecurse(curr, max, minDur, maxDur int) {
	time.Sleep(time.Duration(rand.Intn(maxDur-minDur) + minDur))
	if curr == max {
		return
	}

	if rand.Intn(2)&1 == 1 {
		curr++
		randomRecurse(curr, max, minDur, maxDur)
	}
}
