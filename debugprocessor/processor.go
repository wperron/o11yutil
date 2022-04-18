package debugprocessor

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	defaultWriter                        = os.Stdout
	_             sdktrace.SpanProcessor = &Processor{}
)

// Processor is an implementation of trace.SpanSyncer that writes spans to stdout.
type Processor struct {
	// Output Writer used to print new spans to.
	out io.Writer
}

func (p *Processor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {}

func (p *Processor) OnEnd(s sdktrace.ReadOnlySpan) {
	fmt.Fprintf(p.out, "%s::%s{%s}\n",
		s.InstrumentationLibrary().Name,
		s.Name(),
		kvToString(s),
	)
}
func (p *Processor) ForceFlush(ctx context.Context) error { return nil }
func (p *Processor) Shutdown(ctx context.Context) error   { return nil }

func kvToString(s sdktrace.ReadOnlySpan) string {
	kv := s.Attributes()
	asStrings := make([]string, 0, len(kv)+1)
	for _, pair := range kv {
		asStrings = append(asStrings, fmt.Sprintf("%s=%s", pair.Key, pair.Value.Emit()))
	}
	asStrings = append(asStrings, fmt.Sprintf("%s=%d", "latency", s.EndTime().Sub(s.StartTime()).Milliseconds()))
	return strings.Join(asStrings, ", ")
}
