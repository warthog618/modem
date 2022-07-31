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
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
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
	"github.com/warthog618/sms/encoding/ucs2"
)

var debug = false // set to true to enable tracing of the flow to the mockModem.

const (
	sub = "\x1a"
	esc = "\x1b"
)

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
		{
			"WithTextMode",
			[]gsm.Option{gsm.WithTextMode},
			true,
		},
		{
			"WithNumericErrors",
			[]gsm.Option{gsm.WithNumericErrors},
			true,
		},
		{
			"WithTextualErrors",
			[]gsm.Option{gsm.WithTextualErrors},
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
		esc + "\r\n\r\n": {"\r\n"},
		"ATZ\r\n":        {"OK\r\n"},
		"ATE0\r\n":       {"OK\r\n"},
		// for init (GSM)
		"AT+CMEE=2\r\n": {"OK\r\n"},
		"AT+CMEE=1\r\n": {"OK\r\n"},
		"AT+CMGF=1\r\n": {"OK\r\n"},
		"AT+GCAP\r\n":   {"+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
	}
	patterns := []struct {
		name     string
		options  []at.InitOption
		residual []byte
		key      string
		value    []string
		gopts    []gsm.Option
		err      error
	}{
		{
			"vanilla",
			nil,
			nil,
			"",
			nil,
			[]gsm.Option{gsm.WithTextMode},
			nil,
		},
		{
			"residual OKs",
			nil,
			[]byte("\r\nOK\r\nOK\r\n"),
			"",
			nil,
			[]gsm.Option{gsm.WithTextMode},
			nil,
		},
		{
			"residual ERRORs",
			nil,
			[]byte("\r\nERROR\r\nERROR\r\n"),
			"",
			nil,
			[]gsm.Option{gsm.WithTextMode},
			nil,
		},
		{
			"cruft",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"cruft\r\n", "+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"},
			[]gsm.Option{gsm.WithTextMode},
			nil,
		},
		{
			"CMEE textual error",
			nil,
			nil,
			"AT+CMEE=2\r\n",
			[]string{"ERROR\r\n"},
			[]gsm.Option{gsm.WithTextMode},
			at.ErrError,
		},
		{
			"CMEE numeric error",
			nil,
			nil,
			"AT+CMEE=1\r\n",
			[]string{"ERROR\r\n"},
			[]gsm.Option{gsm.WithTextMode, gsm.WithNumericErrors},
			at.ErrError,
		},
		{
			"GCAP error",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"ERROR\r\n"},
			[]gsm.Option{gsm.WithTextMode},
			at.ErrError,
		},
		{
			"not GSM capable",
			nil,
			nil,
			"AT+GCAP\r\n",
			[]string{"+GCAP: +DS,+ES\r\n", "OK\r\n"},
			[]gsm.Option{gsm.WithTextMode},
			gsm.ErrNotGSMCapable,
		},
		{
			"AT init failure",
			nil,
			nil,
			"ATZ\r\n",
			[]string{"ERROR\r\n"},
			[]gsm.Option{gsm.WithTextMode},
			fmt.Errorf("ATZ returned error: %w", at.ErrError),
		},
		{
			"timeout",
			[]at.InitOption{at.WithTimeout(0)},
			nil,
			"",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			at.ErrDeadlineExceeded,
		},
		{
			"unsupported PDU mode",
			nil,
			nil,
			"",
			nil,
			[]gsm.Option{gsm.WithPDUMode},
			at.ErrError,
		},
		{
			"PDU mode",
			nil,
			nil,
			"AT+CMGF=0\r\n",
			[]string{"OK\r\n"},
			[]gsm.Option{gsm.WithPDUMode},
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
			g := gsm.New(a, p.gopts...)
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
		"AT+CMGS=\"+123456789\"\r": {"\n>"},
		"AT+CMGS=23\r":             {"\n>"},
		"test message" + sub:       {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"cruft test message" + sub: {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"000101099121436587f900000cf4f29c0e6a97e7f3f0b90c" + sub: {"\r\n", "+CMGS: 44\r\n", "\r\nOK\r\n"},
		"malformed test message" + sub:                           {"\r\n", "pad\r\n", "\r\nOK\r\n"},
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
			[]gsm.Option{gsm.WithTextMode},
			"+123456789",
			"test message",
			nil,
			"42",
		},
		{
			"error",
			nil,
			[]gsm.Option{gsm.WithTextMode},
			"+1234567890",
			"test message",
			at.ErrError,
			"",
		},
		{
			"cruft",
			nil,
			[]gsm.Option{gsm.WithTextMode},
			"+123456789",
			"cruft test message",
			nil,
			"43",
		},
		{
			"malformed",
			nil,
			[]gsm.Option{gsm.WithTextMode},
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
			nil,
			"+123456789",
			"a very long test message that will not fit within one SMS PDU as it is just too long for one PDU even with GSM encoding, though you can fit more in one PDU than you may initially expect",
			gsm.ErrOverlength,
			"",
		},
		{
			"encode error",
			nil,
			[]gsm.Option{
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
		"000101099121436587f900000cf4f29c0e6a97e7f3f0b90c" + sub: {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"004101099121436587f90000a0050003010201c2207b599e07b1dfee33885e9ed341edf27c1e3e97417474980ebaa7d96c90fb4d0799d374d03d4d47a7dda0b7bb0c9a36a72028b10a0acf41693a283d07a9eb733a88fe7e83d86ff719647ecb416f771904255641657bd90dbaa7e968d071da0495dde33739ed3eb34074f4bb7e4683f2ef3a681c7683cc693aa8fd9697416937e8ed2e83a0" + sub: {"\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"004102099121436587f90000270500030102028855101d1d7683f2ef3aa81dce83d2ee343d1d66b3f3a0321e5e1ed301" + sub: {"\r\n", "+CMGS: 44\r\n", "\r\nOK\r\n"},
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
			[]gsm.Option{gsm.WithTextMode},
			"+123456789",
			"test message",
			gsm.ErrWrongMode,
			nil,
		},
		{
			"error",
			nil,
			nil,
			"+1234567890",
			"test message",
			at.ErrError,
			nil,
		},
		{
			"malformed",
			nil,
			nil,
			"+123456789",
			"malformed test message",
			gsm.ErrMalformedResponse,
			nil,
		},
		{
			"timeout",
			[]at.CommandOption{at.WithTimeout(0)},
			nil,
			"+123456789",
			"test message",
			at.ErrDeadlineExceeded,
			nil,
		},
		{
			"one pdu",
			nil,
			nil,
			"+123456789",
			"test message",
			nil,
			[]string{"42"},
		},
		{
			"two pdu",
			nil,
			nil,
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
			[]gsm.Option{gsm.WithEncoderOption(sms.AsUCS2)},
			"+123456789",
			"an odd length string!",
			tpdu.EncodeError("SmsSubmit.ud.sm", tpdu.ErrOddUCS2Length),
			nil,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g, mm := setupModem(t, cmdSet, p.goptions...)
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
		"AT+CMGS=6\r":          {"\n>"},
		"00010203040506" + sub: {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"00110203040506" + sub: {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"00210203040506" + sub: {"\r\n", "pad\r\n", "\r\nOK\r\n"},
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
	g, mm = setupModem(t, cmdSet, gsm.WithTextMode)
	defer teardownModem(mm)
	p := patterns[0]
	omr, oerr := g.SendPDU(p.tpdu)
	assert.Equal(t, gsm.ErrWrongMode, oerr)
	assert.Equal(t, "", omr)
}

func TestStartMessageRx(t *testing.T) {
	cmdSet := map[string][]string{
		"AT+CNMA\r\n": {"\r\nOK\r\n"},
	}
	g, mm := setupModem(t, cmdSet, gsm.WithTextMode)
	teardownModem(mm)

	msgChan := make(chan gsm.Message, 3)
	errChan := make(chan error, 3)
	mh := func(msg gsm.Message) {
		msgChan <- msg
	}
	eh := func(err error) {
		errChan <- err
	}

	// wrong mode
	err := g.StartMessageRx(mh, eh)
	require.Equal(t, gsm.ErrWrongMode, err)

	g, mm = setupModem(t, cmdSet)
	defer teardownModem(mm)

	// fails CNMA
	err = g.StartMessageRx(mh, eh)
	require.Equal(t, at.ErrError, err)

	cmdSet["AT+CNMI=1,2,0,0,0\r\n"] = []string{"\r\nOK\r\n"}
	cmdSet["AT+CSMS=1\r\n"] = []string{"\r\nOK\r\n"}

	// pass
	err = g.StartMessageRx(mh, eh)
	require.Nil(t, err)

	// already exists
	err = g.StartMessageRx(mh, eh)
	require.Equal(t, at.ErrIndicationExists, err)

	// CMT patterns to exercise cmtHandler
	patterns := []struct {
		rx  string
		msg gsm.Message
		err error
	}{
		{
			"+CMT: ,24\r\n00040B911234567890F000000250100173832305C8329BFD06\r\n",
			gsm.Message{
				Number:  "+21436587090",
				Message: "Hello",
				SCTS: tpdu.Timestamp{
					Time: time.Date(2020, time.May, 1, 10, 37, 38, 0, time.FixedZone("any", 8*3600))},
			},
			nil,
		},
		{
			"+CMT: ,2X\r\n00040B911234567JUNK000000250100173832305C8329BFD06\r\n",
			gsm.Message{Message: "no message received"},
			gsm.ErrUnmarshal{
				Info: []string{
					"+CMT: ,2X",
					"00040B911234567JUNK000000250100173832305C8329BFD06",
				},
				Err: &strconv.NumError{Func: "Atoi", Num: "2X", Err: strconv.ErrSyntax},
			},
		},
		{
			"+CMT: ,27\r\n004400000000101010000000000f050003030206906174181d468701\r\n",
			gsm.Message{Message: "no message received"},
			gsm.ErrCollect{
				TPDU: tpdu.TPDU{
					FirstOctet: 0x44,
					SCTS: tpdu.Timestamp{
						Time: time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)},
					UDH: tpdu.UserDataHeader{
						tpdu.InformationElement{ID: 0, Data: []byte{3, 2, 6}},
					},
					UD: []byte("Hahahaha"),
				},
				Err: sms.ErrReassemblyInconsistency,
			},
		},
		{
			"+CMT: ,19\r\n0004000000081010100000000006d83dde01d83d\r\n",
			gsm.Message{Message: "no message received"},
			gsm.ErrDecode{
				TPDUs: []*tpdu.TPDU{
					{
						FirstOctet: 4,
						DCS:        0x08,
						SCTS: tpdu.Timestamp{
							Time: time.Date(2001, time.January, 1, 0, 0, 0, 0, time.FixedZone("any", 0))},
						UD: []byte{0xd8, 0x3d, 0xde, 0x01, 0xd8, 0x3d},
					},
				},
				Err: ucs2.ErrDanglingSurrogate([]byte{0xd8, 0x3d}),
			},
		},
	}
	for _, p := range patterns {
		mm.r <- []byte(p.rx)
		select {
		case msg := <-msgChan:
			assert.Equal(t, p.msg.Number, msg.Number)
			assert.Equal(t, p.msg.Message, msg.Message)
			assert.Equal(t, p.msg.SCTS.Unix(), msg.SCTS.Unix())
		case err := <-errChan:
			require.IsType(t, p.err, err)
			switch v := err.(type) {
			case gsm.ErrCollect:
				xerr := p.err.(gsm.ErrCollect)
				assert.Equal(t, xerr.TPDU, v.TPDU)
				assert.Equal(t, xerr.Err, v.Err)
			case gsm.ErrDecode:
				xerr := p.err.(gsm.ErrDecode)
				assert.Equal(t, xerr.Err, v.Err)
			case gsm.ErrUnmarshal:
				xerr := p.err.(gsm.ErrUnmarshal)
				assert.Equal(t, xerr.Info, v.Info)
				assert.Equal(t, xerr.Err, v.Err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("no notification received")
		}
	}
}

func TestStartMessageRxOptions(t *testing.T) {
	cmdSet := map[string][]string{
		"AT+CSMS=1\r\n":         {"\r\nOK\r\n"},
		"AT+CNMI=1,2,0,0,0\r\n": {"\r\nOK\r\n"},
		"AT+CNMI=1,2,0,0,1\r\n": {"\r\nOK\r\n"},
		"AT+CNMA\r\n":           {"\r\nOK\r\n"},
	}

	msgChan := make(chan gsm.Message, 3)
	errChan := make(chan error, 3)
	mh := func(msg gsm.Message) {
		msgChan <- msg
	}
	eh := func(err error) {
		errChan <- err
	}

	mc := mockCollector{
		errChan: errChan,
		err:     errors.New("mock collector expiry"),
	}
	sfs := tpdu.TPDU{
		FirstOctet: tpdu.FoUDHI,
		OA:         tpdu.Address{Addr: "1234", TOA: 0x91},
		SCTS: tpdu.Timestamp{
			Time: time.Date(2017, time.August, 31, 11, 21, 54, 0, time.FixedZone("any", 8*3600)),
		},
		UDH: tpdu.UserDataHeader{
			tpdu.InformationElement{ID: 0, Data: []byte{2, 2, 1}},
		},
		UD: []byte("a short first segment"),
	}
	sfsb, _ := sfs.MarshalBinary()
	sfsh := hex.EncodeToString(sfsb)
	sfsi := fmt.Sprintf("+CMT: ,%d\r\n00%s\r\n", len(sfsh)/2, sfsh)
	patterns := []struct {
		name    string
		options []gsm.RxOption
		err     error
		expire  bool
	}{
		{
			"default",
			nil,
			nil,
			false,
		},
		{
			"timeout",
			[]gsm.RxOption{gsm.WithReassemblyTimeout(time.Microsecond)},
			gsm.ErrReassemblyTimeout{TPDUs: []*tpdu.TPDU{&sfs, nil}},
			true,
		},
		{
			"collector",
			[]gsm.RxOption{gsm.WithCollector(mc)},
			mc.err,
			true,
		},
		{
			"initCmds",
			[]gsm.RxOption{gsm.WithInitCmds("+CNMI=1,2,0,0,1")},
			nil,
			false,
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			g, mm := setupModem(t, cmdSet)
			defer teardownModem(mm)
			err := g.StartMessageRx(mh, eh, p.options...)
			require.Nil(t, err)
			mm.r <- []byte(sfsi)
			select {
			case msg := <-msgChan:
				t.Errorf("received message: %v", msg)
			case err := <-errChan:
				require.IsType(t, p.err, err)
				if _, ok := err.(gsm.ErrReassemblyTimeout); !ok {
					assert.Equal(t, p.err, err)
				} else {
					assert.Equal(t, p.err.Error(), err.Error())
				}
			case <-time.After(100 * time.Millisecond):
				assert.False(t, p.expire)
			}
		}
		t.Run(p.name, f)
	}
}

func TestStopMessageRx(t *testing.T) {
	cmdSet := map[string][]string{
		"AT+CSMS=1\r\n":         {"\r\nOK\r\n"},
		"AT+CNMI=1,2,0,0,0\r\n": {"\r\nOK\r\n"},
		"AT+CNMI=0,0,0,0,0\r\n": {"\r\nOK\r\n"},
		"AT+CNMA\r\n":           {"\r\nOK\r\n"},
	}
	g, mm := setupModem(t, cmdSet)
	mm.echo = false
	defer teardownModem(mm)

	msgChan := make(chan gsm.Message, 3)
	errChan := make(chan error, 3)
	mh := func(msg gsm.Message) {
		msgChan <- msg
	}
	eh := func(err error) {
		errChan <- err
	}
	err := g.StartMessageRx(mh, eh)
	require.Nil(t, err)
	mm.r <- []byte("+CMT: ,24\r\n00040B911234567890F000000250100173832305C8329BFD06\r\n")
	select {
	case <-msgChan:
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}

	// stop
	g.StopMessageRx()

	// would return a msg
	mm.r <- []byte("+CMT: ,24\r\n00040B911234567890F000000250100173832305C8329BFD06\r\n")
	select {
	case msg := <-msgChan:
		t.Errorf("msg received: %v", msg)
	case err := <-errChan:
		t.Errorf("error received: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// would return an error
	mm.r <- []byte("+CMT: ,13\r\n00040B911234567890\r\n")
	select {
	case msg := <-msgChan:
		t.Errorf("msg received: %v", msg)
	case err := <-errChan:
		t.Errorf("error received: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnmarshalTPDU(t *testing.T) {
	patterns := []struct {
		name string
		info []string
		tpdu tpdu.TPDU
		err  error
	}{
		{
			"ok",
			[]string{
				"+CMT: ,24",
				"00040B911234567890F000000250100173832305C8329BFD06",
			},
			tpdu.TPDU{
				FirstOctet: 0x04,
				OA:         tpdu.Address{TOA: 145, Addr: "21436587090"},
				UD:         []byte("Hello"),
			},
			nil,
		},
		{
			"underlength",
			[]string{
				"+CMT: ,2X",
			},
			tpdu.TPDU{},
			gsm.ErrUnderlength,
		},
		{
			"bad length",
			[]string{
				"+CMT: ,2X",
				"00040B911234567JUNK000000250100173832305C8329BFD06",
			},
			tpdu.TPDU{},
			&strconv.NumError{Func: "Atoi", Num: "2X", Err: strconv.ErrSyntax},
		},
		{
			"mismatched length",
			[]string{
				"+CMT: ,24",
				"00040B911234567890F00000",
			},
			tpdu.TPDU{},
			fmt.Errorf("length mismatch - expected %d, got %d", 24, 11),
		},
		{
			"not hex",
			[]string{
				"+CMT: ,24",
				"00040B911234567JUNK000000250100173832305C8329BFD06",
			},
			tpdu.TPDU{},
			hex.InvalidByteError(0x4a),
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			tp, err := gsm.UnmarshalTPDU(p.info)
			tp.SCTS = tpdu.Timestamp{}
			assert.Equal(t, p.tpdu, tp)
			assert.Equal(t, p.err, err)
		}
		t.Run(p.name, f)
	}
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

func TestErrors(t *testing.T) {
	patterns := []struct {
		name   string
		err    error
		errStr string
	}{
		{
			"collect",
			gsm.ErrCollect{
				TPDU: tpdu.TPDU{
					FirstOctet: 0x44,
					DCS:        0,
					SCTS: tpdu.Timestamp{
						Time: time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)},
					UDH: tpdu.UserDataHeader{
						tpdu.InformationElement{ID: 0, Data: []byte{3, 2, 6}},
					},
					UD: []byte("Hahahaha"),
				},
				Err: errors.New("twisted"),
			},
			"error 'twisted' collecting TPDU: {Direction:0 FirstOctet:68 OA:{TOA:0 Addr:} FCS:0 MR:0 CT:0 MN:0 DA:{TOA:0 Addr:} RA:{TOA:0 Addr:} PI:0 SCTS:2001-01-01 00:00:00 +0000 DT:0001-01-01 00:00:00 +0000 ST:0 PID:0 DCS:0x00 7bit VP:{Format:Not Present Time:0001-01-01 00:00:00 +0000 Duration:0s EFI:0} UDH:[{ID:0 Data:[3 2 6]}] UD:[72 97 104 97 104 97 104 97]}",
		},
		{
			"decode",
			gsm.ErrDecode{
				TPDUs: []*tpdu.TPDU{
					{

						FirstOctet: 4,
						DCS:        0x08,
						SCTS: tpdu.Timestamp{
							Time: time.Date(2001, time.January, 1, 0, 0, 0, 0, time.FixedZone("any", 0))},
						UD: []byte{0xd8, 0x3d, 0xde, 0x01, 0xd8, 0x3d},
					},
				},
				Err: errors.New("dangling surrogate"),
			},
			"error 'dangling surrogate' decoding: &{Direction:0 FirstOctet:4 OA:{TOA:0 Addr:} FCS:0 MR:0 CT:0 MN:0 DA:{TOA:0 Addr:} RA:{TOA:0 Addr:} PI:0 SCTS:2001-01-01 00:00:00 +0000 DT:0001-01-01 00:00:00 +0000 ST:0 PID:0 DCS:0x08 UCS-2 VP:{Format:Not Present Time:0001-01-01 00:00:00 +0000 Duration:0s EFI:0} UDH:[] UD:[216 61 222 1 216 61]}",
		},
		{

			"reassemblyTimeout",
			gsm.ErrReassemblyTimeout{
				TPDUs: []*tpdu.TPDU{
					{

						FirstOctet: 4,
						DCS:        0x08,
						SCTS: tpdu.Timestamp{
							Time: time.Date(2001, time.January, 1, 0, 0, 0, 0, time.FixedZone("any", 0))},
						UD: []byte{0xd8, 0x3d, 0xde, 0x01, 0xd8, 0x3d},
					},
				},
			},
			"timeout reassembling: &{Direction:0 FirstOctet:4 OA:{TOA:0 Addr:} FCS:0 MR:0 CT:0 MN:0 DA:{TOA:0 Addr:} RA:{TOA:0 Addr:} PI:0 SCTS:2001-01-01 00:00:00 +0000 DT:0001-01-01 00:00:00 +0000 ST:0 PID:0 DCS:0x08 UCS-2 VP:{Format:Not Present Time:0001-01-01 00:00:00 +0000 Duration:0s EFI:0} UDH:[] UD:[216 61 222 1 216 61]}",
		},
		{
			"unmarshal",
			gsm.ErrUnmarshal{
				Info: []string{
					"+CMT: ,2X",
					"00040B911234567JUNK000000250100173832305C8329BFD06",
				},
				Err: errors.New("bent"),
			},
			"error 'bent' unmarshalling: +CMT: ,2X\n00040B911234567JUNK000000250100173832305C8329BFD06\n",
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			assert.Equal(t, p.errStr, p.err.Error())
		}
		t.Run(p.name, f)
	}
}

type mockModem struct {
	cmdSet    map[string][]string
	echo      bool
	closed    bool
	readDelay time.Duration
	// The buffer emulating characters emitted by the modem.
	r chan []byte
}

func (mm *mockModem) Read(p []byte) (n int, err error) {
	data, ok := <-mm.r
	if data == nil {
		return 0, at.ErrClosed
	}
	time.Sleep(mm.readDelay)
	copy(p, data) // assumes p is empty
	if !ok {
		return len(data), fmt.Errorf("closed with data")
	}
	return len(data), nil
}

func (mm *mockModem) Write(p []byte) (n int, err error) {
	if mm.closed {
		return 0, at.ErrClosed
	}
	if mm.echo {
		mm.r <- p
	}
	v := mm.cmdSet[string(p)]
	if len(v) == 0 {
		mm.r <- []byte("\r\nERROR\r\n")
	} else {
		for _, l := range v {
			if len(l) == 0 {
				continue
			}
			mm.r <- []byte(l)
		}
	}
	return len(p), nil
}

func (mm *mockModem) Close() error {
	if mm.closed == false {
		mm.closed = true
		close(mm.r)
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

func teardownModem(mm *mockModem) {
	mm.Close()
}

type mockCollector struct {
	errChan chan<- error
	err     error
}

func (c mockCollector) Collect(t tpdu.TPDU) ([]*tpdu.TPDU, error) {
	c.errChan <- c.err
	return nil, nil
}
