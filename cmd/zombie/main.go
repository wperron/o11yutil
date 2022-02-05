// Copyright 2021 William Perron. All rights reserved. MIT License.

// Command zombie is a natural load generator to simulate real-life traffic
// on a system.
package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/fatih/color"
	"github.com/go-kit/kit/log"
	"github.com/wperron/o11yutil/api"
	"github.com/wperron/o11yutil/client"
	"github.com/wperron/o11yutil/config"
)

// Version is set via build flag -ldflags -X main.Version
var (
	Version  string
	Branch   string
	Revision string
	logger   log.Logger
)

var (
	configPath = flag.String("config", "", "The location of the config file.")
	noColor    = flag.Bool("no-color", false, "Suppress colors from the output")
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
	// Set up channel on which to send termination signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

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
			logger.Log(api.Serve(conf.Api.Addr))
		}()
	}

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	out := make(chan client.Result)
	errors := make(chan error)

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
			pinger := client.NewInstrumentedPinger(ns)
			go pinger.Ping(t, out, errors)
		}
	}

	go func() {
		for m := range out {
			vals := []interface{}{"target", m.Name, "method", m.Method, "status", m.Status, "url", m.URL, "latency", m.Latency}
			if m.TraceID != "" {
				vals = append(vals, "trace_id", m.TraceID)
			}
			logger.Log(vals...)
		}
	}()

	go func() {
		for e := range errors {
			logger.Log("error", e)
			os.Exit(1)
		}
	}()

	// Block until a signal is received.
	s := <-sigs
	logger.Log(fmt.Sprintf("Got signal: %s", s))
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

func printSummary(c config.Config) {
	fmt.Println("Zombie started")
	if noColor != nil && *noColor {
		fmt.Printf("version=%s branch=%s revision=%s\n", Version, Branch, Revision)
	} else {
		fmt.Printf("version=%s branch=%s revision=%s\n", color.GreenString(Version), color.GreenString(Branch), color.GreenString(Revision))
	}

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
