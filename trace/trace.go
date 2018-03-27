// Package trace provides a decorator for io.ReadWriter that logs all reads
// and writes.
package trace

import (
	"io"
	"log"
)

// Trace is a trace log on an io.ReadWriter.
// All reads and writes are written to the logger.
type Trace struct {
	rw   io.ReadWriter
	l    *log.Logger
	wfmt string
	rfmt string
}

// Option modifies a Trace object created by New.
type Option func(*Trace)

// New creates a new trace on the io.ReadWriter.
func New(rw io.ReadWriter, l *log.Logger, opts ...Option) *Trace {
	t := &Trace{rw: rw, l: l, wfmt: "w: %s", rfmt: "r: %s"}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// ReadFormat sets the format used for read logs.
func ReadFormat(format string) Option {
	return func(t *Trace) {
		t.rfmt = format
	}
}

// WriteFormat sets the format used for write logs.
func WriteFormat(format string) Option {
	return func(t *Trace) {
		t.wfmt = format
	}
}

func (t *Trace) Read(p []byte) (n int, err error) {
	n, err = t.rw.Read(p)
	if n > 0 {
		t.l.Printf(t.rfmt, p[:n])
	}
	return n, err
}

func (t *Trace) Write(p []byte) (n int, err error) {
	n, err = t.rw.Write(p)
	if n > 0 {
		t.l.Printf(t.wfmt, p[:n])
	}
	return n, err
}
