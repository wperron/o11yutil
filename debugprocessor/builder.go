package debugprocessor

import (
	"io"
)

var defaultBuilder = &Builder{
	w: defaultWriter,
}

type Builder struct {
	w io.Writer
}

func New() *Builder {
	return defaultBuilder
}

func (b *Builder) WithWriter(w io.Writer) *Builder {
	b.w = w
	return b
}

func (b *Builder) Build() *Processor {
	return &Processor{
		out: b.w,
	}
}
