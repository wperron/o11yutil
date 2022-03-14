package debugprocessor

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	defaultWriter                        = os.Stdout
	_             sdktrace.SpanProcessor = &Processor{}
)

// Processor is an implementation of trace.SpanSyncer that writes spans to stdout.
type Processor struct {
	// Output Writer used to print new spans to.
	out io.Writer

	// The sequence of characters to use for intendation. Defaults to 2 spaces.
	indent string

	// TODO(wperron) find a better way to traverse the span graph to find the parent
	spans map[trace.SpanID]sdktrace.ReadWriteSpan
}

func (p *Processor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	if p.out == nil {
		p.out = defaultWriter
	}

	// Record each span in the map. Span IDs are unique, so it shouldn't matter
	// if we ever overwrite the key here.
	p.spans[s.SpanContext().SpanID()] = s

	indent := strings.Repeat(p.indent, p.applyIndentation(s))

	fmt.Fprintf(p.out, "%s%s::%s{%s}\n",
		indent,
		s.InstrumentationLibrary().Name,
		s.Name(),
		kvToString(s.Attributes()),
	)
}

func (p *Processor) OnEnd(s sdktrace.ReadOnlySpan)        {}
func (p *Processor) ForceFlush(ctx context.Context) error { return nil }
func (p *Processor) Shutdown(ctx context.Context) error   { return nil }

func (p *Processor) applyIndentation(s sdktrace.ReadWriteSpan) (lvl int) {
	curr := s
	for curr != nil {
		if next, ok := p.spans[curr.Parent().SpanID()]; ok && next != nil {
			lvl++
			curr = next
			continue
		}
		curr = nil
	}
	return
}

func kvToString(kv []attribute.KeyValue) string {
	asStrings := make([]string, 0, len(kv))
	for _, pair := range kv {
		asStrings = append(asStrings, fmt.Sprintf("%s=%s", pair.Key, pair.Value.Emit()))
	}
	return strings.Join(asStrings, ", ")
}
