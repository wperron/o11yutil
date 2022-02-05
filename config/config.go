// Copyright 2021 William Perron. All rights reserved. MIT License.
package config

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"
)

// Config of the zombie process
type Config struct {
	// API configuration
	Api *Api `yaml:"api,omitempty"`

	// List of Targets
	Targets []Target `yaml:"targets"`
}

// API serving status and metrics info about the zombie process
type Api struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr,omitempty"`
}

// Target to crawl
type Target struct {
	// URL to be requested
	Url string `yaml:"url"`

	// Name to print out in the log, defaults to URL if left empty
	Name string `yaml:"name,omitempty"`

	// Headers to add to the request
	Headers *http.Header `yaml:"headers,omitempty"`

	// Delay to wait between each request. This parameter is affected byt the
	// Jitter parameter. Expressed in milliseconds
	Delay int64 `yaml:"delay"`

	// Jitter applied to the Delay between each request. Jitter is a modifier
	// applied in each direction so that a value of `0.2` means ±20%
	Jitter float64 `yaml:"jitter"`

	// TraceHeader contains the name of the trace ID header. If set, each
	// response will be anotated with it.
	TraceHeader string `yaml:"trace_header,omitempty"`

	// Workers defines how many concurrent goroutines to spawn to generate load
	// concurrently. Defaults to 1.
	Workers int `yaml:"workers,omitempty"`
}

const defaultDuration = 1000 * time.Millisecond

func (t *Target) Duration() time.Duration {
	if t == nil || t.Delay == 0 {
		return defaultDuration
	}

	return time.Duration(t.Delay) * time.Millisecond
}

func Load(s string) (*Config, error) {
	cfg := &Config{}

	if err := yaml.Unmarshal([]byte(s), cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func LoadFile(fp string) (*Config, error) {
	bs, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %s", fp, err)
	}

	cfg, err := Load(string(bs))
	if err != nil {
		return nil, fmt.Errorf("parsing YAML file: %s", err)
	}
	return cfg, nil
}
