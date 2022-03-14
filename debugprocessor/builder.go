package debugprocessor

import (
	"io"
	"os"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var defaultBuilder = &Builder{
	w: os.Stdout,
	i: "  ",
}

type Builder struct {
	w io.Writer
	i string
}

func New() *Builder {
	return defaultBuilder
}

func (b *Builder) WithWriter(w io.Writer) *Builder {
	b.w = w
	return b
}

func (b *Builder) WithIndent(i string) *Builder {
	b.i = i
	return b
}

func (b *Builder) Build() *Processor {
	return &Processor{
		out:    b.w,
		indent: b.i,
		spans:  make(map[trace.SpanID]sdktrace.ReadWriteSpan),
	}
}
