// Copyright 2021 William Perron. All rights reserved. MIT License.

// Command zombie is a natural load generator to simulate real-life traffic
// on a system.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/wperron/o11yutil/api"
	"github.com/wperron/o11yutil/client"
	"github.com/wperron/o11yutil/config"
	"github.com/wperron/o11yutil/debugprocessor"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// Version is set via build flag -ldflags -X main.Version
var (
	Version  string
	Branch   string
	Revision string
	logger   log.Logger
	tracer   trace.Tracer
)

var (
	configPath = flag.String("config", "", "The location of the config file.")
	format     = flag.String("format", "logfmt", "Log output format. Defaults to 'logfmt'")
	// TODO(wperron) add verbose and quiet options
)

func init() {
	if client.DefaultPinger == nil {
		fmt.Println("default pinger is nil")
		os.Exit(1)
	}
}

func main() {
	// Initialize context with signal.NotifyContext, this context will watch
	// for the listed signals before sending <-Done()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Parse command line args
	flag.Parse()

	// Load the configuration file
	conf, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	printSummary(*conf)

	logger, err = makeLogger(*format, os.Stdout)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Start the API if enabled
	if conf.Api != nil && conf.Api.Enabled {
		go func() {
			if err := logger.Log(api.Serve(conf.Api.Addr)); err != nil {
				fmt.Println("error serving api:", err)
			}
		}()
	}

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	shut, err := initTracing(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer shut() // nolint
	tracer = otel.Tracer("zombie")

	for _, t := range conf.Targets {
		ns := t.Name
		if ns == "" {
			ns = t.Url
		}

		workers := t.Workers
		if workers <= 0 {
			workers = 1
		}

		for i := 0; i < workers; i++ {
			pinger := client.NewInstrumentedPinger(ns, tracer)
			go pinger.Ping(ctx, t)
		}
	}

	// Block until a signal is received.
	s := <-ctx.Done()
	_ = logger.Log(fmt.Sprintf("Got signal: %s", s))
}

func makeLogger(f string, out io.Writer) (log.Logger, error) {
	switch f {
	case "logfmt":
		return log.NewLogfmtLogger(log.NewSyncWriter(out)), nil
	case "json":
		return log.NewJSONLogger(log.NewSyncWriter(out)), nil
	default:
		return nil, errors.New("unknown log format")
	}
}

type shutdown func() error

// initTracing initializes the OpenTelemetry stdout exporter.
func initTracing(ctx context.Context) (shutdown, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("zombie"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating otel resource: %v", err)
	}

	debug := debugprocessor.New().WithWriter(os.Stdout).Build()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(debug),
	)
	otel.SetTracerProvider(tracerProvider)

	return func() error {
		if err := tracerProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("stopping tracer provider: %v", err)
		}
		return nil
	}, nil
}

func printSummary(c config.Config) {
	fmt.Println("Zombie started")
	fmt.Printf("version=%s branch=%s revision=%s\n", Version, Branch, Revision)

	if c.Api != nil && c.Api.Enabled {
		fmt.Printf("API enabled on %s\n", c.Api.Addr)
	}

	for _, t := range c.Targets {
		if t.Name != "" {
			fmt.Printf("target name: %s at %s, base delay: %d ms, jitter: %f\n", t.Name, t.Url, t.Duration().Milliseconds(), t.Jitter)
		} else {
			fmt.Printf("target name: %s, base delay: %d ms, jitter: %f\n", t.Url, t.Duration().Milliseconds(), t.Jitter)
		}
	}
	fmt.Println("")
}
