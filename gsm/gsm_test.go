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
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/trace"
	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/semioctet"
	"github.com/warthog618/sms/encoding/tpdu"
)

var debug = false // set to true to enable tracing of the flow to the mockModem.

func TestNew(t *testing.T) {
	mm := mockModem{cmdSet: nil, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := at.New(&mm)
	patterns := []struct {
		name    string
		options []gsm.Option
		success bool
	}{
		{
			"default",
			nil,
			true,
		},
		{
			"WithPDUMode",
			[]gsm.Option{gsm.WithPDUMode},
			true,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g := gsm.New(a, p.options...)
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
		"ATE0\r\n":              {"OK\r\n"},
		// for init (GSM)
		"AT+CMEE=2\r\n": {"OK\r\n"},
		"AT+CMGF=1\r\n": {"OK\r\n"},
		"AT+GCAP\r\n":   {"+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
	}
	patterns := []struct {
		name     string
		options  []at.InitOption
		residual []byte
		key      string
		value    []string
		pduMode  bool
		err      error
	}{
		{
			"vanilla",
			nil,
			nil,
			"",
			nil,
			false,
			nil,
		},
		{
			"residual OKs",
			nil,
			[]byte("\r\nOK\r\nOK\r\n"),
			"",
			nil,
			false,
			nil,
		},
		{
			"residual ERRORs",
			nil,
			[]byte("\r\nERROR\r\nERROR\r\n"),
			"",
			nil,
			false,
			nil,
		},
		{
			"cruft",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"cruft\r\n", "+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
			false,
			nil,
		},
		{
			"CMEE error",
			nil,
			nil,
			"AT+CMEE=2\r\n",
			[]string{"ERROR\r\n"},
			false,
			at.ErrError,
		},
		{
			"GCAP error",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"ERROR\r\n"},
			false,
			at.ErrError,
		},
		{
			"not GSM capable",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"+GCAP: +DS,+ES\r\n", "OK\r\n"},
			false,
			gsm.ErrNotGSMCapable,
		},
		{
			"AT init failure",
			nil,
			nil,
			"ATZ\r\n",
			[]string{"ERROR\r\n"},
			false,
			fmt.Errorf("ATZ returned error: %w", at.ErrError),
		},
		{
			"timeout",
			[]at.InitOption{at.WithTimeout(0)},
			nil,
			"",
			nil,
			false,
			at.ErrDeadlineExceeded,
		},
		{
			"unsupported PDU mode",
			nil,
			nil,
			"",
			nil,
			true,
			at.ErrError,
		},
		{
			"PDU mode",
			nil,
			nil,
			"AT+CMGF=0\r\n",
			[]string{"OK\r\n"},
			true,
			nil,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			mm := mockModem{
				cmdSet:    cmdSet,
				echo:      false,
				r:         make(chan []byte, 10),
				readDelay: time.Millisecond,
			}
			defer teardownModem(&mm)
			a := at.New(&mm)
			gopts := []gsm.Option{}
			if p.pduMode {
				gopts = append(gopts, gsm.WithPDUMode)
			}
			g := gsm.New(a, gopts...)
			require.NotNil(t, g)
			var oldvalue []string
			if p.residual != nil {
				mm.r <- p.residual
			}
			if p.key != "" {
				oldvalue = cmdSet[p.key]
				cmdSet[p.key] = p.value
			}
			err := g.Init(p.options...)
			if oldvalue != nil {
				cmdSet[p.key] = oldvalue
			}
			assert.Equal(t, p.err, err)
		}
		t.Run(p.name, f)
	}
}

func TestSendShortMessage(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=\"+123456789\"\r":        {"\n>"},
		"AT+CMGS=23\r":                    {"\n>"},
		"test message" + string(26):       {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"cruft test message" + string(26): {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"000101099121436587f900000cf4f29c0e6a97e7f3f0b90c" + string(26): {"\r\n", "+CMGS: 44\r\n", "\r\nOK\r\n"},
		"malformed test message" + string(26):                           {"\r\n", "pad\r\n", "\r\nOK\r\n"},
	}
	patterns := []struct {
		name     string
		options  []at.CommandOption
		goptions []gsm.Option
		number   string
		message  string
		err      error
		mr       string
	}{
		{
			"ok",
			nil,
			nil,
			"+123456789",
			"test message",
			nil,
			"42",
		},
		{
			"error",
			nil,
			nil,
			"+1234567890",
			"test message",
			at.ErrError,
			"",
		},
		{
			"cruft",
			nil,
			nil,
			"+123456789",
			"cruft test message",
			nil,
			"43",
		},
		{
			"malformed",
			nil,
			nil,
			"+123456789",
			"malformed test message",
			gsm.ErrMalformedResponse,
			"",
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			nil,
			"+123456789",
			"test message",
			at.ErrDeadlineExceeded,
			"",
		},
		{
			"pduMode",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"test message",
			nil,
			"44",
		},
		{
			"overlength",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"a very long test message that will not fit within one SMS PDU as it is just too long for one PDU even with GSM encoding, though you can fit more in one PDU than you may initially expect",
			gsm.ErrOverlength,
			"",
		},
		{
			"encode error",
			nil,
			[]gsm.Option{
				gsm.WithPDUMode,
				gsm.WithEncoderOption(sms.WithTemplateOption(tpdu.DCS(0x80))),
			},
			"+123456789",
			"test message",
			sms.ErrDcsConflict,
			"",
		},
		{
			"marshal error",
			nil,
			[]gsm.Option{gsm.WithPDUMode, gsm.WithEncoderOption(sms.AsUCS2)},
			"+123456789",
			"an odd length string!",
			tpdu.EncodeError("SmsSubmit.ud.sm", tpdu.ErrOddUCS2Length),
			"",
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g, mm := setupModem(t, cmdSet, p.goptions...)
			require.NotNil(t, g)
			require.NotNil(t, mm)
			defer teardownModem(mm)

			mr, err := g.SendShortMessage(p.number, p.message, p.options...)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.mr, mr)
		}
		t.Run(p.name, f)
	}
}

func TestSendLongMessage(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=23\r":  {"\n>"},
		"AT+CMGS=152\r": {"\n>"},
		"AT+CMGS=47\r":  {"\n>"},
		"AT+CMGS=32\r":  {"\r\n", "pad\r\n", "\r\nOK\r\n"},
		"000101099121436587f900000cf4f29c0e6a97e7f3f0b90c" + string(26): {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"004101099121436587f90000a0050003010201c2207b599e07b1dfee33885e9ed341edf27c1e3e97417474980ebaa7d96c90fb4d0799d374d03d4d47a7dda0b7bb0c9a36a72028b10a0acf41693a283d07a9eb733a88fe7e83d86ff719647ecb416f771904255641657bd90dbaa7e968d071da0495dde33739ed3eb34074f4bb7e4683f2ef3a681c7683cc693aa8fd9697416937e8ed2e83a0" + string(26): {"\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"004102099121436587f90000270500030102028855101d1d7683f2ef3aa81dce83d2ee343d1d66b3f3a0321e5e1ed301" + string(26): {"\r\n", "+CMGS: 44\r\n", "\r\nOK\r\n"},
	}
	patterns := []struct {
		name     string
		options  []at.CommandOption
		goptions []gsm.Option
		number   string
		message  string
		err      error
		mr       []string
	}{
		{
			"text mode",
			nil,
			nil,
			"+123456789",
			"test message",
			gsm.ErrWrongMode,
			nil,
		},
		{
			"error",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+1234567890",
			"test message",
			at.ErrError,
			nil,
		},
		{
			"malformed",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"malformed test message",
			gsm.ErrMalformedResponse,
			nil,
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"test message",
			at.ErrDeadlineExceeded,
			nil,
		},
		{
			"one pdu",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"test message",
			nil,
			[]string{"42"},
		},
		{
			"two pdu",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			"+123456789",
			"a very long test message that will not fit within one SMS PDU as it is just too long for one PDU even with GSM encoding, though you can fit more in one PDU than you may initially expect",
			nil,
			[]string{"43", "44"},
		},
		{
			"encode error",
			nil,
			[]gsm.Option{
				gsm.WithPDUMode,
				gsm.WithEncoderOption(sms.WithTemplateOption(tpdu.DCS(0x80))),
			},
			"+123456789",
			"test message",
			sms.ErrDcsConflict,
			nil,
		},
		{
			"marshal error",
			nil,
			[]gsm.Option{gsm.WithPDUMode, gsm.WithEncoderOption(sms.AsUCS2)},
			"+123456789",
			"an odd length string!",
			tpdu.EncodeError("SmsSubmit.ud.sm", tpdu.ErrOddUCS2Length),
			nil,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g, mm := setupModem(t, cmdSet, p.goptions...)
			require.NotNil(t, g)
			require.NotNil(t, mm)
			defer teardownModem(mm)

			mr, err := g.SendLongMessage(p.number, p.message, p.options...)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.mr, mr)
		}
		t.Run(p.name, f)
	}
}

func TestSendPDU(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=6\r":                 {"\n>"},
		"00010203040506" + string(26): {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"00110203040506" + string(26): {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"00210203040506" + string(26): {"\r\n", "pad\r\n", "\r\nOK\r\n"},
	}
	patterns := []struct {
		name    string
		options []at.CommandOption
		tpdu    []byte
		err     error
		mr      string
	}{
		{
			"ok",
			nil,
			[]byte{1, 2, 3, 4, 5, 6},
			nil,
			"42",
		},
		{
			"error",
			nil,
			[]byte{1},
			at.ErrError,
			"",
		},
		{
			"cruft",
			nil,
			[]byte{0x11, 2, 3, 4, 5, 6},
			nil,
			"43",
		},
		{
			"malformed",
			nil,
			[]byte{0x21, 2, 3, 4, 5, 6},
			gsm.ErrMalformedResponse,
			"",
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			[]byte{1, 2, 3, 4, 5, 6},
			at.ErrDeadlineExceeded,
			"",
		},
	}
	g, mm := setupModem(t, cmdSet, gsm.WithPDUMode)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)

	for _, p := range patterns {
		f := func(t *testing.T) {
			mr, err := g.SendPDU(p.tpdu, p.options...)
			assert.Equal(t, p.err, err)
			assert.Equal(t, p.mr, mr)
		}
		t.Run(p.name, f)
	}

	// wrong mode
	g, mm = setupModem(t, cmdSet)
	require.NotNil(t, g)
	require.NotNil(t, mm)
	defer teardownModem(mm)
	p := patterns[0]
	omr, oerr := g.SendPDU(p.tpdu)
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
	omr, oerr := g.SendPDU(tp)
	assert.Equal(t, tpdu.EncodeError("addr", semioctet.ErrInvalidDigit(0x74)), oerr)
	assert.Equal(t, "", omr)
}

type mockModem struct {
	cmdSet    map[string][]string
	echo      bool
	closed    bool
	readDelay time.Duration
	// The buffer emulating characters emitted by the modem.
	r chan []byte
}

func (m *mockModem) Read(p []byte) (n int, err error) {
	data, ok := <-m.r
	if data == nil {
		return 0, fmt.Errorf("closed")
	}
	time.Sleep(m.readDelay)
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
	mm := &mockModem{
		cmdSet:    cmdSet,
		echo:      true,
		r:         make(chan []byte, 10),
		readDelay: time.Millisecond,
	}
	var modem io.ReadWriter = mm
	if debug {
		modem = trace.New(modem)
	}
	g := gsm.New(at.New(modem), gopts...)
	require.NotNil(t, g)
	return g, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
