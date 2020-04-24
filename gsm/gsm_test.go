// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

//
// Test suite for GSM module.
//
// Note that these tests provide a mockModem which does not attempt to emulate
// a serial modem, but which provides responses required to exercise gsm.go So,
// while the commands may follow the structure of the AT protocol they most
// certainly are not AT commands - just patterns that elicit the behaviour
// required for the test.

package gsm_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/trace"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/semioctet"
	"github.com/warthog618/sms/encoding/tpdu"
)

var debug = false // set to true to enable tracing of the flow to the mockModem.

func TestNew(t *testing.T) {
	mm := mockModem{cmdSet: nil, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	patterns := []struct {
		name    string
		options []gsm.Option
		success bool
	}{
		{
			"default",
			nil,
			false,
		},
		{
			"FromReaderWriter",
			[]gsm.Option{gsm.FromReadWriter(&mm)},
			true,
		},
		{
			"FromAT",
			[]gsm.Option{gsm.FromAT(at.New(&mm))},
			true,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g := gsm.New(p.options...)
			if p.success {
				assert.NotNil(t, g)
			} else {
				assert.Nil(t, g)
			}
		}
		t.Run(p.name, f)
	}
}

func TestInit(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		// for init (AT)
		string(27) + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":               {"OK\r\n"},
		"AT^CURC=0\r\n":         {"OK\r\n"},
		// for init (GSM)
		"AT+CMEE=2\r\n": {"OK\r\n"},
		"AT+CMGF=1\r\n": {"OK\r\n"},
		"AT+GCAP\r\n":   {"+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
	}
	background := context.Background()
	cancelled, cancel := context.WithCancel(background)
	cancel()
	timeout, cancel := context.WithTimeout(background, 0)
	patterns := []struct {
		name     string
		ctx      context.Context
		residual []byte
		key      string
		value    []string
		pduMode  bool
		err      error
	}{
		{
			"vanilla",
			background,
			nil, "",
			nil,
			false,
			nil,
		},
		{
			"residual OKs",
			background,
			[]byte("\r\nOK\r\nOK\r\n"),
			"",
			nil,
			false,
			nil,
		},
		{
			"residual ERRORs",
			background,
			[]byte("\r\nERROR\r\nERROR\r\n"),
			"",
			nil,
			false,
			nil,
		},
		{
			"cruft",
			background,
			nil,
			"AT+GCAP\r\n",
			[]string{"cruft\r\n", "+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
			false,
			nil,
		},
		{
			"CMEE error",
			background,
			nil,
			"AT+CMEE=2\r\n",
			[]string{"ERROR\r\n"},
			false,
			at.ErrError,
		},
		{
			"GCAP error",
			background,
			nil,
			"AT+GCAP\r\n",
			[]string{"ERROR\r\n"},
			false,
			at.ErrError,
		},
		{
			"not GSM capable",
			background,
			nil,
			"AT+GCAP\r\n",
			[]string{"+GCAP: +DS,+ES\r\n", "OK\r\n"},
			false,
			gsm.ErrNotGSMCapable,
		},
		{
			"AT init failure",
			background,
			nil,
			"ATZ\r\n",
			[]string{"ERROR\r\n"},
			false,
			fmt.Errorf("ATZ returned error: %w", at.ErrError),
		},
		{
			"cancelled",
			cancelled,
			nil,
			"",
			nil,
			false,
			context.Canceled,
		},
		{
			"timeout",
			timeout,
			nil,
			"",
			nil,
			false,
			context.DeadlineExceeded,
		},
		{
			"unsupported PDU mode",
			background,
			nil,
			"",
			nil,
			true,
			at.ErrError,
		},
		{
			"PDU mode",
			background,
			nil,
			"AT+CMGF=0\r\n",
			[]string{"OK\r\n"},
			true,
			nil,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
			defer teardownModem(&mm)
			gopts := []gsm.Option{gsm.FromReadWriter(&mm)}
			if p.pduMode {
				gopts = append(gopts, gsm.WithPDUMode)
			}
			g := gsm.New(gopts...)
			require.NotNil(t, g)
			var oldvalue []string
			if p.residual != nil {
				mm.r <- p.residual
			}
			if p.key != "" {
				oldvalue = cmdSet[p.key]
				cmdSet[p.key] = p.value
			}
			err := g.Init(p.ctx)
			if oldvalue != nil {
				cmdSet[p.key] = oldvalue
			}
			assert.Equal(t, p.err, err)
		}
		t.Run(p.name, f)
	}
	cancel()
}

func TestSendSMS(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=\"+123456789\"\r":            {"\n>"},
		"test message" + string(26):           {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"cruft test message" + string(26):     {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"malformed test message" + string(26): {"\r\n", "pad\r\n", "\r\nOK\r\n"},
	}
	background := context.Background()
	cancelled, cancel := context.WithCancel(background)
	cancel()
	timeout, cancel := context.WithTimeout(background, 0)
	patterns := []struct {
		name    string
		ctx     context.Context
		number  string
		message string
		err     error
		mr      string
	}{
		{
			"ok",
			background,
			"+123456789",
			"test message",
			nil,
			"42",
		},
		{
			"error",
			background,
			"+1234567890",
			"test message",
			at.ErrError,
			"",
		},
		{
			"cruft",
			background,
			"+123456789",
			"cruft test message",
			nil,
			"43",
		},
		{
			"malformed",
			background,
			"+123456789",
			"malformed test message",
			gsm.ErrMalformedResponse,
			"",
		},
		{
			"cancelled",
			cancelled,
			"+123456789",
			"test message",
			context.Canceled,
			"",
		},
		{
			"timeout",
			timeout,
			"+123456789",
			"test message",
			context.DeadlineExceeded,
			"",
		},
	}
	g, mm := setupModem(t, cmdSet)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)

	for _, p := range patterns {
		f := func(t *testing.T) {
			mr, err := g.SendSMS(p.ctx, p.number, p.message)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.mr, mr)
		}
		t.Run(p.name, f)
	}
	cancel()

	// wrong mode
	g, mm = setupModem(t, cmdSet, gsm.WithPDUMode)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)
	p := patterns[0]
	mr, err := g.SendSMS(p.ctx, p.number, p.message)
	assert.Equal(t, gsm.ErrWrongMode, err)
	assert.Equal(t, "", mr)
}

func TestSendSMSPDU(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=6\r":                 {"\n>"},
		"00010203040506" + string(26): {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"00110203040506" + string(26): {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"00210203040506" + string(26): {"\r\n", "pad\r\n", "\r\nOK\r\n"},
	}
	background := context.Background()
	cancelled, cancel := context.WithCancel(background)
	cancel()
	timeout, cancel := context.WithTimeout(background, 0)
	patterns := []struct {
		name string
		ctx  context.Context
		tpdu []byte
		err  error
		mr   string
	}{
		{
			"ok",
			background,
			[]byte{1, 2, 3, 4, 5, 6},
			nil,
			"42",
		},
		{
			"error",
			background,
			[]byte{1},
			at.ErrError,
			"",
		},
		{
			"cruft",
			background,
			[]byte{0x11, 2, 3, 4, 5, 6},
			nil,
			"43",
		},
		{
			"malformed",
			background,
			[]byte{0x21, 2, 3, 4, 5, 6},
			gsm.ErrMalformedResponse,
			"",
		},
		{
			"cancelled",
			cancelled,
			[]byte{1, 2, 3, 4, 5, 6},
			context.Canceled,
			"",
		},
		{
			"timeout",
			timeout,
			[]byte{1, 2, 3, 4, 5, 6},
			context.DeadlineExceeded,
			"",
		},
	}
	g, mm := setupModem(t, cmdSet, gsm.WithPDUMode)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)

	for _, p := range patterns {
		f := func(t *testing.T) {
			mr, err := g.SendSMSPDU(p.ctx, p.tpdu)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.mr, mr)
		}
		t.Run(p.name, f)
	}
	cancel()

	// wrong mode
	g, mm = setupModem(t, cmdSet)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)
	p := patterns[0]
	omr, oerr := g.SendSMSPDU(p.ctx, p.tpdu)
	assert.Equal(t, gsm.ErrWrongMode, oerr)
	assert.Equal(t, "", omr)
}

func TestWithSCA(t *testing.T) {
	var sca pdumode.SMSCAddress
	sca.Addr = "text"
	gopts := []gsm.Option{gsm.WithSCA(sca)}
	g, mm := setupModem(t, nil, gopts...)
	defer teardownModem(mm)

	tp := []byte{1, 2, 3, 4, 5, 6}
	ctx := context.Background()
	omr, oerr := g.SendSMSPDU(ctx, tp)
	assert.Equal(t, tpdu.EncodeError("addr", semioctet.ErrInvalidDigit(0x74)), oerr)
	assert.Equal(t, "", omr)
}

type mockModem struct {
	cmdSet map[string][]string
	echo   bool
	closed bool
	// The buffer emulating characters emitted by the modem.
	r chan []byte
}

func (m *mockModem) Read(p []byte) (n int, err error) {
	data, ok := <-m.r
	if data == nil {
		return 0, fmt.Errorf("closed")
	}
	copy(p, data) // assumes p is empty
	if !ok {
		return len(data), fmt.Errorf("closed with data")
	}
	return len(data), nil
}

func (m *mockModem) Write(p []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("closed")
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

func setupModem(t *testing.T, cmdSet map[string][]string, gopts ...gsm.Option) (*gsm.GSM, *mockModem) {
	mm := &mockModem{cmdSet: cmdSet, echo: true, r: make(chan []byte, 10)}
	var modem io.ReadWriter = mm
	if debug {
		modem = trace.New(modem)
	}
	gopts = append(gopts, gsm.FromReadWriter(modem))
	g := gsm.New(gopts...)
	require.NotNil(t, g)
	return g, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
