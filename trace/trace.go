// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package trace provides a decorator for io.ReadWriter that logs all reads
// and writes.
package trace

import (
	"io"
	"log"
	"os"
)

// Trace is a trace log on an io.ReadWriter.
//
// All reads and writes are written to the logger.
type Trace struct {
	rw   io.ReadWriter
	l    Logger
	wfmt string
	rfmt string
}

// Logger defines the interface used to log trace messages.
type Logger interface {
	Printf(format string, v ...interface{})
}

// Option modifies a Trace object created by New.
type Option func(*Trace)

// New creates a new trace on the io.ReadWriter.
func New(rw io.ReadWriter, options ...Option) *Trace {
	t := &Trace{
		rw:   rw,
		wfmt: "w: %s",
		rfmt: "r: %s",
	}
	for _, option := range options {
		option(t)
	}
	if t.l == nil {
		t.l = log.New(os.Stdout, "", log.LstdFlags)
	}
	return t
}

// WithReadFormat sets the format used for read logs.
func WithReadFormat(format string) Option {
	return func(t *Trace) {
		t.rfmt = format
	}
}

// WithWriteFormat sets the format used for write logs.
func WithWriteFormat(format string) Option {
	return func(t *Trace) {
		t.wfmt = format
	}
}

// WithLogger specifies the logger to be used to log trace messages.
//
// By default traces are logged to Stdout.
func WithLogger(l Logger) Option {
	return func(t *Trace) {
		t.l = l
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
