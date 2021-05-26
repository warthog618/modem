// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

package trace_test

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vasjaj/modem/trace"
)

func TestNew(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", log.LstdFlags)
	// vanilla
	tr := trace.New(mrw)
	assert.NotNil(t, tr)

	// with options
	tr = trace.New(mrw, trace.WithLogger(l), trace.WithReadFormat("r: %v"))
	assert.NotNil(t, tr)
}

func TestRead(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := trace.New(mrw, trace.WithLogger(l))
	require.NotNil(t, tr)
	i := make([]byte, 10)
	n, err := tr.Read(i)
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte("r: one\n"), b.Bytes())
}

func TestWrite(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := trace.New(mrw, trace.WithLogger(l))
	require.NotNil(t, tr)
	n, err := tr.Write([]byte("two"))
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte("w: two\n"), b.Bytes())
}

func TestReadFormat(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := trace.New(mrw, trace.WithLogger(l), trace.WithReadFormat("R: %v"))
	require.NotNil(t, tr)
	i := make([]byte, 10)
	n, err := tr.Read(i)
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte("R: [111 110 101]\n"), b.Bytes())
}

func TestWriteFormat(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := trace.New(mrw, trace.WithLogger(l), trace.WithWriteFormat("W: %v"))
	require.NotNil(t, tr)
	n, err := tr.Write([]byte("two"))
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte("W: [116 119 111]\n"), b.Bytes())
}
