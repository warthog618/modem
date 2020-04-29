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
		{
			"timeout",
			[]at.Option{at.WithTimeout(10 * time.Millisecond)},
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
		"ATE0\r\n":              {"OK\r\n"},
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

			start := time.Now()
			err := a.Init()
			assert.Nil(t, err)
			end := time.Now()
			assert.GreaterOrEqual(t, int64(end.Sub(start)), int64(p.d))
		}
		t.Run(p.name, f)
	}
}

func TestWithCmds(t *testing.T) {
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"OK\r\n"},
		"ATE0\r\n":              {"OK\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
	}
	patterns := []struct {
		name    string
		options []at.Option
	}{
		{
			"default",
			nil,
		},
		{
			"cmd",
			[]at.Option{at.WithCmds("Z")},
		},
		{
			"cmds",
			[]at.Option{at.WithCmds("Z", "Z", "^CURC=0")},
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
			defer teardownModem(&mm)
			a := at.New(&mm, p.options...)
			require.NotNil(t, a)

			err := a.Init()
			assert.Nil(t, err)
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
		"ATE0\r\n":              {"OK\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
	}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	require.NotNil(t, a)
	err := a.Init()
	require.Nil(t, err)
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
	}

	// residual OKs
	mm.r <- []byte("\r\nOK\r\nOK\r\n")
	err = a.Init()
	assert.Nil(t, err)

	// residual ERRORs
	mm.r <- []byte("\r\nERROR\r\nERROR\r\n")
	err = a.Init()
	assert.Nil(t, err)

	// customised commands
	err = a.Init(at.WithCmds("Z", "Z", "^CURC=0"))
	assert.Nil(t, err)
}

func TestInitFailure(t *testing.T) {
	cmdSet := map[string][]string{
		// for init
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"ERROR\r\n"},
		"ATE0\r\n":              {"OK\r\n"},
	}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	require.NotNil(t, a)
	err := a.Init()
	assert.NotNil(t, err)
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
	}

	// lone E0 should work
	err = a.Init(at.WithCmds("E0"))
	assert.Nil(t, err)
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
	err := a.Init(at.WithTimeout(10 * time.Millisecond))
	assert.Equal(t, at.ErrDeadlineExceeded, err)
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
	patterns := []struct {
		name    string
		options []at.CommandOption
		cmd     string
		mutator func()
		info    []string
		err     error
	}{
		{
			"empty",
			nil,
			"",
			nil,
			nil,
			nil,
		},
		{
			"pass",
			nil,
			"PASS",
			nil,
			nil,
			nil,
		},
		{
			"info",
			nil,
			"INFO=1",
			nil,
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"err",
			nil,
			"ERR",
			nil,
			nil,
			at.ErrError,
		},
		{
			"cms",
			nil,
			"CMS",
			nil,
			nil,
			at.CMSError("204"),
		},
		{
			"cme",
			nil,
			"CME",
			nil,
			nil,
			at.CMEError("42"),
		},
		{
			"dial ok",
			nil,
			"D1",
			nil,
			[]string{"CONNECT: 57600"},
			nil,
		},
		{
			"dial busy",
			nil,
			"D2",
			nil,
			[]string{"info1"},
			at.ConnectError("BUSY"),
		},
		{
			"dial no answer",
			nil,
			"D3",
			nil,
			nil,
			at.ConnectError("NO ANSWER"),
		},
		{
			"dial no carrier",
			nil,
			"D4",
			nil,
			nil,
			at.ConnectError("NO CARRIER"),
		},
		{
			"dial no dialtone",
			nil,
			"D5",
			nil,
			nil,
			at.ConnectError("NO DIALTONE"),
		},
		{
			"no echo",
			nil,
			"INFO=1",
			func() { mm.echo = false },
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			"",
			func() { mm.readDelay = time.Millisecond },
			nil,
			at.ErrDeadlineExceeded,
		},
		{
			"write error",
			nil,
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
			nil,
			"NULL",
			func() {
				mm.closeOnWrite = true
			},
			nil,
			at.ErrClosed,
		},
		{
			"closed before request",
			nil,
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
			info, err := m.Command(p.cmd, p.options...)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.info, info)
		}
		t.Run(p.name, f)
	}
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
	info, err := m.Command("PASS")
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, info)

	// closed before request
	info, err = m.Command("PASS")
	assert.Equal(t, at.ErrClosed, err)
	assert.Nil(t, info)
}

func TestCommandClosedPreWrite(t *testing.T) {
	// retest this case separately to catch closure on the write to modem.
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)
	mm.Close()
	// closed before request
	info, err := m.Command("PASS")
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
	patterns := []struct {
		name    string
		options []at.CommandOption
		cmd1    string
		cmd2    string
		mutator func()
		info    []string
		err     error
	}{
		{
			"empty",
			nil,
			"",
			"",
			nil,
			nil,
			at.ErrError,
		},
		{
			"ok",
			nil,
			"SMS",
			"sms+",
			nil,
			[]string{"info4", "info5", "INFO: info6"},
			nil,
		},
		{
			"info",
			nil,
			"SMS",
			"info",
			nil,
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"err",
			nil,
			"ERR",
			"errsms",
			nil,
			nil,
			at.ErrError,
		},
		{
			"cms",
			nil,
			"CMS",
			"cmssms",
			nil,
			nil,
			at.CMSError("204"),
		},
		{
			"cme",
			nil,
			"CME",
			"cmesms",
			nil,
			nil,
			at.CMEError("42"),
		},
		{
			"no echo",
			nil,
			"SMS2",
			"info",
			func() { mm.echo = false },
			[]string{"info1", "info2", "INFO: info3"},
			nil,
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			"SMS2",
			"info",
			func() { mm.readDelay = time.Millisecond },
			nil,
			at.ErrDeadlineExceeded,
		},
		{
			"write error",
			nil,
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
			nil,
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
			nil,
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
			info, err := m.SMSCommand(p.cmd1, p.cmd2, p.options...)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.info, info)
		}
		t.Run(p.name, f)
	}
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
	done := make(chan struct{})
	// Need to queue multiple commands to check queued commands code path.
	go func() {
		info, err := m.SMSCommand("SMS", "closed")
		assert.NotNil(t, err)
		assert.Nil(t, info)
		close(done)
	}()
	info, err := m.SMSCommand("SMS", "closed")
	assert.NotNil(t, err)
	assert.Nil(t, info)
	<-done
}

func TestAddIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c := make(chan []string)
	handler := func(info []string) {
		c <- info
	}
	err := m.AddIndication("notify", handler)
	assert.Nil(t, err)
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
	err = m.AddIndication("notify", handler)
	assert.Equal(t, at.ErrIndicationExists, err)

	err = m.AddIndication("foo", handler, at.WithTrailingLines(2))
	assert.Nil(t, err)
	mm.r <- []byte("foo:\r\nbar\r\nbaz\r\n")
	select {
	case n := <-c:
		assert.Equal(t, []string{"foo:", "bar", "baz"}, n)
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}
}

func TestWithIndication(t *testing.T) {
	c := make(chan []string)
	handler := func(info []string) {
		c <- info
	}
	m, mm := setupModem(t,
		nil,
		at.WithIndication("notify", handler),
		at.WithIndication("foo", handler, at.WithTrailingLines(2)))
	defer teardownModem(mm)

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
	err := m.AddIndication("notify", handler)
	assert.Equal(t, at.ErrIndicationExists, err)

	mm.r <- []byte("foo:\r\nbar\r\nbaz\r\n")
	select {
	case n := <-c:
		assert.Equal(t, []string{"foo:", "bar", "baz"}, n)
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}
}

func TestCancelIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c := make(chan []string)
	handler := func(info []string) {
		c <- info
	}
	err := m.AddIndication("notify", handler)
	assert.Nil(t, err)

	err = m.AddIndication("foo", handler, at.WithTrailingLines(2))
	assert.Nil(t, err)

	m.CancelIndication("notify")
	mm.r <- []byte("foo:\r\nbar\r\nbaz\r\n")
	select {
	case n := <-c:
		assert.Equal(t, []string{"foo:", "bar", "baz"}, n)
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}

	mm.Close()
	select {
	case <-time.After(10 * time.Millisecond):
		t.Fatal("modem failed to close")
	case <-m.Closed():
	}
	// for coverage of cancel while closed
	m.CancelIndication("foo")
}

func TestAddIndicationClose(t *testing.T) {
	handler := func(info []string) {
		t.Error("returned partial info")
	}
	m, mm := setupModem(t, nil,
		at.WithIndication("foo:", handler, at.WithTrailingLines(2)))
	defer teardownModem(mm)

	mm.r <- []byte("foo:\r\nbar\r\n")
	mm.Close()
	select {
	case <-m.Closed():
	case <-time.After(100 * time.Millisecond):
		t.Error("modem still open")
	}
}

func TestAddIndicationClosed(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	handler := func(info []string) {
	}
	mm.Close()
	select {
	case <-time.After(10 * time.Millisecond):
		t.Fatal("modem failed to close")
	case <-m.Closed():
	}
	err := m.AddIndication("notify", handler)
	assert.Equal(t, at.ErrClosed, err)
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
	readDelay        time.Duration
	// The buffer emulating characters emitted by the modem.
	r chan []byte
}

func (m *mockModem) Read(p []byte) (n int, err error) {
	data, ok := <-m.r
	if data == nil {
		return 0, at.ErrClosed
	}
	time.Sleep(m.readDelay)
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

func setupModem(t *testing.T, cmdSet map[string][]string, options ...at.Option) (*at.AT, *mockModem) {
	mm := &mockModem{cmdSet: cmdSet, echo: true, r: make(chan []byte, 10)}
	var modem io.ReadWriter = mm
	debug := false // set to true to enable tracing of the flow to the mockModem.
	if debug {
		modem = trace.New(modem)
	}
	a := at.New(modem, options...)
	require.NotNil(t, a)
	return a, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
