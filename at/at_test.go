// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

//  Test suite for AT module.
//
//  Note that these tests provide a mockModem which does not attempt to emulate
//  a serial modem, but which provides responses required to exercise at.go So,
//  while the commands may follow the structure of the AT protocol they most
//  certainly are not AT commands - just patterns that elicit the behaviour
//  required for the test.

package at_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/trace"
)

func TestNew(t *testing.T) {
	patterns := []struct {
		name    string
		options []at.Option
	}{
		{
			"default",
			nil,
		},
		{
			"escTime",
			[]at.Option{at.WithEscTime(100 * time.Millisecond)},
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			// mocked
			mm := mockModem{cmdSet: nil, echo: false, r: make(chan []byte, 10)}
			defer teardownModem(&mm)
			a := at.New(&mm, p.options...)
			require.NotNil(t, a)
			select {
			case <-a.Closed():
				t.Error("modem closed")
			default:
			}
		}
		t.Run(p.name, f)
	}
}

func TestWithEscTime(t *testing.T) {
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"OK\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
	}
	patterns := []struct {
		name    string
		options []at.Option
		d       time.Duration
	}{
		{
			"default",
			nil,
			20 * time.Millisecond,
		},
		{
			"100ms",
			[]at.Option{at.WithEscTime(100 * time.Millisecond)},
			100 * time.Millisecond,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
			defer teardownModem(&mm)
			a := at.New(&mm, p.options...)
			require.NotNil(t, a)

			ctx := context.Background()
			start := time.Now()
			err := a.Init(ctx)
			assert.Nil(t, err)
			end := time.Now()
			assert.GreaterOrEqual(t, int64(end.Sub(start)), int64(p.d))
		}
		t.Run(p.name, f)
	}
}

func TestInit(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"OK\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
	}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	require.NotNil(t, a)
	ctx := context.Background()
	err := a.Init(ctx)
	require.Nil(t, err)
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
	}

	// residual OKs
	mm.r <- []byte("\r\nOK\r\nOK\r\n")
	err = a.Init(ctx)
	assert.Nil(t, err)

	// residual ERRORs
	mm.r <- []byte("\r\nERROR\r\nERROR\r\n")
	err = a.Init(ctx)
	assert.Nil(t, err)
}

func TestInitFailure(t *testing.T) {
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"ERROR\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
	}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	require.NotNil(t, a)
	ctx := context.Background()
	err := a.Init(ctx)
	assert.NotNil(t, err)
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
	}
}

func TestCloseInInitTimeout(t *testing.T) {
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {""},
	}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	require.NotNil(t, a)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := a.Init(ctx)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestCommand(t *testing.T) {
	cmdSet := map[string][]string{
		"AT\r\n":       {"OK\r\n"},
		"ATPASS\r\n":   {"OK\r\n"},
		"ATINFO=1\r\n": {"info1\r\n", "info2\r\n", "INFO: info3\r\n", "\r\n", "OK\r\n"},
		"ATCMS\r\n":    {"+CMS ERROR: 204\r\n"},
		"ATCME\r\n":    {"+CME ERROR: 42\r\n"},
		"ATD1\r\n":     {"CONNECT: 57600\r\n"},
		"ATD2\r\n":     {"info1\r\n", "BUSY\r\n"},
		"ATD3\r\n":     {"NO ANSWER\r\n"},
		"ATD4\r\n":     {"NO CARRIER\r\n"},
		"ATD5\r\n":     {"NO DIALTONE\r\n"},
	}
	m, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)
	background := context.Background()
	cancelled, cancel := context.WithCancel(background)
	cancel()
	timeout, cancel := context.WithTimeout(background, 0)
	patterns := []struct {
		name    string
		ctx     context.Context
		cmd     string
		mutator func()
		info    []string
		err     error
	}{
		{
			"empty",
			background,
			"",
			nil,
			nil,
			nil,
		},
		{
			"pass",
			background,
			"PASS",
			nil,
			nil,
			nil,
		},
		{
			"info",
			background,
			"INFO=1",
			nil,
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"err",
			background,
			"ERR",
			nil,
			nil,
			at.ErrError,
		},
		{
			"cms",
			background,
			"CMS",
			nil,
			nil,
			at.CMSError("204"),
		},
		{
			"cme",
			background,
			"CME",
			nil,
			nil,
			at.CMEError("42"),
		},
		{
			"dial ok",
			background,
			"D1",
			nil,
			[]string{"CONNECT: 57600"},
			nil,
		},
		{
			"dial busy",
			background,
			"D2",
			nil,
			[]string{"info1"},
			at.ConnectError("BUSY"),
		},
		{
			"dial no answer",
			background,
			"D3",
			nil,
			nil,
			at.ConnectError("NO ANSWER"),
		},
		{
			"dial no carrier",
			background,
			"D4",
			nil,
			nil,
			at.ConnectError("NO CARRIER"),
		},
		{
			"dial no dialtone",
			background,
			"D5",
			nil,
			nil,
			at.ConnectError("NO DIALTONE"),
		},
		{
			"no echo",
			background,
			"INFO=1",
			func() { mm.echo = false },
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"timeout",
			timeout,
			"",
			nil,
			nil,
			context.DeadlineExceeded,
		},
		{
			"cancelled",
			cancelled,
			"",
			func() {
				m, mm = setupModem(t, cmdSet)
			},
			nil,
			context.Canceled,
		},
		{
			"write error",
			background,
			"PASS",
			func() {
				m, mm = setupModem(t, cmdSet)
				mm.errOnWrite = true
			},
			nil,
			errors.New("Write error"),
		},
		{
			"closed before response",
			background,
			"NULL",
			func() {
				mm.closeOnWrite = true
			},
			nil,
			at.ErrClosed,
		},
		{
			"closed before request",
			background,
			"PASS",
			func() { <-m.Closed() },
			nil,
			at.ErrClosed,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			if p.mutator != nil {
				p.mutator()
			}
			info, err := m.Command(p.ctx, p.cmd)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.info, info)
		}
		t.Run(p.name, f)
	}
	cancel()
}

func TestCommandClosedIdle(t *testing.T) {
	// retest this case separately to catch closure while cmdProcessor is idle.
	// (otherwise that code path can be skipped)
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)
	mm.Close()
	select {
	case <-m.Closed():
	case <-time.Tick(10 * time.Millisecond):
		t.Error("Timeout waiting for modem to close")
	}
}

func TestCommandClosedOnWrite(t *testing.T) {
	// retest this case separately to catch closure on the write to modem.
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)
	mm.closeOnWrite = true
	ctx := context.Background()
	info, err := m.Command(ctx, "PASS")
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, info)

	// closed before request
	info, err = m.Command(ctx, "PASS")
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, info)
}

func TestCommandClosedPreWrite(t *testing.T) {
	// retest this case separately to catch closure on the write to modem.
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)
	mm.Close()
	ctx := context.Background()
	// closed before request
	info, err := m.Command(ctx, "PASS")
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, info)
}

func TestSMSCommand(t *testing.T) {
	cmdSet := map[string][]string{
		"ATCMS\r":           {"\r\n+CMS ERROR: 204\r\n"},
		"ATCME\r":           {"\r\n+CME ERROR: 42\r\n"},
		"ATSMS\r":           {"\n>"},
		"ATSMS2\r":          {"\n> "},
		"info" + string(26): {"\r\n", "info1\r\n", "info2\r\n", "INFO: info3\r\n", "\r\n", "OK\r\n"},
		"sms+" + string(26): {"\r\n", "info4\r\n", "info5\r\n", "INFO: info6\r\n", "\r\n", "OK\r\n"},
	}
	m, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)
	background := context.Background()
	cancelled, cancel := context.WithCancel(background)
	cancel()
	timeout, cancel := context.WithTimeout(background, 0)
	patterns := []struct {
		name    string
		ctx     context.Context
		cmd1    string
		cmd2    string
		mutator func()
		info    []string
		err     error
	}{
		{
			"empty",
			background,
			"",
			"",
			nil,
			nil,
			at.ErrError,
		},
		{
			"ok",
			background,
			"SMS",
			"sms+",
			nil,
			[]string{"info4", "info5", "INFO: info6"},
			nil,
		},
		{
			"info",
			background,
			"SMS",
			"info",
			nil,
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"err",
			background,
			"ERR",
			"errsms",
			nil,
			nil,
			at.ErrError,
		},
		{
			"cms",
			background,
			"CMS",
			"cmssms",
			nil,
			nil,
			at.CMSError("204"),
		},
		{
			"cme",
			background,
			"CME",
			"cmesms",
			nil,
			nil,
			at.CMEError("42"),
		},
		{
			"no echo",
			background,
			"SMS2",
			"info",
			func() { mm.echo = false },
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"timeout",
			timeout,
			"SMS2",
			"info",
			nil,
			nil,
			context.DeadlineExceeded,
		},
		{
			"cancelled",
			cancelled,
			"SMS2",
			"info",
			func() {
				m, mm = setupModem(t, cmdSet)
			},
			nil,
			context.Canceled,
		},
		{
			"write error",
			background,
			"EoW",
			"errOnWrite",
			func() {
				m, mm = setupModem(t, cmdSet)
				mm.errOnWrite = true
			},
			nil,
			errors.New("Write error"),
		},
		{
			"closed before response",
			background,
			"CoW",
			"closeOnWrite",
			func() {
				mm.closeOnWrite = true
			},
			nil,
			at.ErrClosed,
		},
		{
			"closed before request",
			background,
			"C",
			"closed",
			func() { <-m.Closed() },
			nil,
			at.ErrClosed,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			if p.mutator != nil {
				p.mutator()
			}
			info, err := m.SMSCommand(p.ctx, p.cmd1, p.cmd2)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.info, info)
		}
		t.Run(p.name, f)
	}
	cancel()
}

func TestSMSCommandClosedPrePDU(t *testing.T) {
	// test case where modem closes between SMS prompt and PDU.
	cmdSet := map[string][]string{
		"ATSMS\r": {"\n>"},
	}
	m, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)
	mm.echo = false
	mm.closeOnSMSPrompt = true
	ctx := context.Background()
	done := make(chan struct{})
	// Need to queue multiple commands to check queued commands code path.
	go func() {
		info, err := m.SMSCommand(ctx, "SMS", "closed")
		assert.NotNil(t, err)
		assert.Nil(t, info)
		close(done)
	}()
	info, err := m.SMSCommand(ctx, "SMS", "closed")
	assert.NotNil(t, err)
	assert.Nil(t, info)
	<-done
}

func TestAddIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c, err := m.AddIndication("notify", 0)
	assert.Nil(t, err)
	require.NotNil(t, c)
	select {
	case n := <-c:
		t.Errorf("got notification without write: %v", n)
	default:
	}
	mm.r <- []byte("notify: :yfiton\r\n")
	select {
	case n := <-c:
		assert.Equal(t, []string{"notify: :yfiton"}, n)
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}
	c2, err := m.AddIndication("notify", 0)
	assert.Equal(t, at.ErrIndicationExists, err)
	assert.Nil(t, c2, "shouldn't return channel on error")
	c2, err = m.AddIndication("foo", 2)
	assert.Nil(t, err)
	require.NotNil(t, c2)
	mm.r <- []byte("foo:\r\nbar\r\nbaz\r\n")
	select {
	case n := <-c2:
		assert.Equal(t, []string{"foo:", "bar", "baz"}, n)
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}
	mm.Close()
	select {
	case <-c:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel still open")
	}
	select {
	case <-c2:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel 2 still open")
	}
	c2, err = m.AddIndication("foo", 2)
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, c2, "shouldn't return channel on error")
}

func TestCancelIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c, err := m.AddIndication("notify", 0)
	assert.Nil(t, err)
	require.NotNil(t, c)
	c2, err := m.AddIndication("foo", 2)
	assert.Nil(t, err)
	require.NotNil(t, c2)
	m.CancelIndication("notify")
	select {
	case <-c:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel still open")
	}
	mm.Close()
	select {
	case <-c2:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel still open")
	}
	// for coverage of cancel while closed
	m.CancelIndication("foo")

}

func TestAddIndicationClose(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c, err := m.AddIndication("foo:", 2)
	assert.Nil(t, err)
	require.NotNil(t, c)
	mm.r <- []byte("foo:\r\nbar\r\n")
	mm.Close()
	select {
	case <-c:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel 2 still open")
	}
}

func TestCMEError(t *testing.T) {
	patterns := []string{"1", "204", "42"}
	for _, p := range patterns {
		f := func(t *testing.T) {
			err := at.CMEError(p)
			expected := fmt.Sprintf("CME Error: %s", string(err))
			assert.Equal(t, expected, err.Error())
		}
		t.Run(fmt.Sprintf("%x", p), f)
	}
}

func TestCMSError(t *testing.T) {
	patterns := []string{"1", "204", "42"}
	for _, p := range patterns {
		f := func(t *testing.T) {
			err := at.CMSError(p)
			expected := fmt.Sprintf("CMS Error: %s", string(err))
			assert.Equal(t, expected, err.Error())
		}
		t.Run(fmt.Sprintf("%x", p), f)
	}
}

func TestConnectError(t *testing.T) {
	patterns := []string{"1", "204", "42"}
	for _, p := range patterns {
		f := func(t *testing.T) {
			err := at.ConnectError(p)
			expected := fmt.Sprintf("Connect: %s", string(err))
			assert.Equal(t, expected, err.Error())
		}
		t.Run(fmt.Sprintf("%x", p), f)
	}
}

type mockModem struct {
	cmdSet           map[string][]string
	closeOnWrite     bool
	closeOnSMSPrompt bool
	errOnWrite       bool
	echo             bool
	closed           bool
	// The buffer emulating characters emitted by the modem.
	r chan []byte
}

func (m *mockModem) Read(p []byte) (n int, err error) {
	data, ok := <-m.r
	if data == nil {
		return 0, at.ErrClosed
	}
	copy(p, data) // assumes p is empty
	if !ok {
		return len(data), errors.New("closed with data")
	}
	return len(data), nil
}

func (m *mockModem) Write(p []byte) (n int, err error) {
	if m.closed {
		return 0, at.ErrClosed
	}
	if m.closeOnWrite {
		m.closeOnWrite = false
		m.Close()
		return len(p), nil
	}
	if m.errOnWrite {
		return 0, errors.New("Write error")
	}
	if m.echo {
		m.r <- p
	}
	v := m.cmdSet[string(p)]
	if len(v) == 0 {
		m.r <- []byte("\r\nERROR\r\n")
	} else {
		for _, l := range v {
			if len(l) == 0 {
				continue
			}
			m.r <- []byte(l)
			if m.closeOnSMSPrompt && len(l) > 1 && l[1] == '>' {
				m.Close()
			}
		}
	}
	return len(p), nil
}

func (m *mockModem) Close() error {
	if m.closed == false {
		m.closed = true
		close(m.r)
	}
	return nil
}

func setupModem(t *testing.T, cmdSet map[string][]string) (*at.AT, *mockModem) {
	mm := &mockModem{cmdSet: cmdSet, echo: true, r: make(chan []byte, 10)}
	var modem io.ReadWriter = mm
	debug := false // set to true to enable tracing of the flow to the mockModem.
	if debug {
		modem = trace.New(modem)
	}
	a := at.New(modem)
	require.NotNil(t, a)
	return a, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
