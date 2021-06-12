package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

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
			Buckets: []float64{.001, .005, .01, 0.025, .05, 0.1, 0.5, 1, 5, 10},
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
	fmt.Fprint(w, "Hello, World!")
}
