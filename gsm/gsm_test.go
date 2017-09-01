/*
  Test suite for GSM module.

	Note that these tests provide a mockModem which does not attempt to emulate
	a serial modem, but which provides responses required to exercise gsm.go
	So, while the commands may follow the structure of the AT protocol they
	most certainly are not AT commands - just patterns that elicit the behaviour
	required for the test.
*/
package gsm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/warthog618/modem/trace"
)

func TestNew(t *testing.T) {
	// mocked
	mm := mockModem{cmdSet: nil, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	g := New(&mm)
	if g == nil {
		t.Fatal("New failed")
	}
	select {
	case <-g.Closed():
		t.Error("modem closed")
	default:
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
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	g := New(&mm)
	if g == nil {
		t.Fatal("New failed")
	}
	select {
	case <-g.Closed():
		t.Error("modem closed")
	default:
	}
	ctx := context.Background()
	// vanilla
	err := g.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}
	// residual OKs
	mm.r <- []byte("\r\nOK\r\nOK\r\n")
	err = g.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}
	// residual ERRORs
	mm.r <- []byte("\r\nERROR\r\nERROR\r\n")
	err = g.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}

	// init failure (CMEE)
	cmdSet["AT+CMEE=2\r\n"] = []string{"ERROR\r\n"}
	err = g.Init(ctx)
	if err == nil {
		t.Error("init succeeded")
	}

	// GCAP req failure
	cmdSet["AT+GCAP\r\n"] = []string{"ERROR\r\n"}
	err = g.Init(ctx)
	if err == nil {
		t.Error("init succeeded")
	}

	// GCAP bad length
	cmdSet["AT+GCAP\r\n"] = []string{"+GCAP: +DS,+ES\r\n", "blah\r\n", "OK\r\n"}
	err = g.Init(ctx)
	if err == nil {
		t.Error("init succeeded")
	}

	// Not GSM capable
	cmdSet["AT+GCAP\r\n"] = []string{"+GCAP: +DS,+ES\r\n", "OK\r\n"}
	err = g.Init(ctx)
	if err == nil {
		t.Error("init succeeded")
	}

	// AT init failure
	cmdSet["ATZ\r\n"] = []string{"ERROR\r\n"}
	err = g.Init(ctx)
	if err == nil {
		t.Error("init succeeded")
	}

	// restored command set to check failures above are not due to something else.
	cmdSet["ATZ\r\n"] = []string{"\r\n", "OK\r\n"}
	cmdSet["AT+GCAP\r\n"] = []string{"+GCAP: +CGSM,+DS,+ES\r\n", "OK\r\n"}
	cmdSet["AT+CMEE=2\r\n"] = []string{"OK\r\n"}
	err = g.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}

	// cancelled
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	err = g.Init(cctx)
	if err == nil {
		t.Error("init succeeded")
	}
	if err != context.Canceled {
		t.Error("init didn't fail with Canceled:", err)
	}

	// timeout
	cctx, cancel = context.WithTimeout(ctx, 0)
	err = g.Init(cctx)
	if err == nil {
		t.Error("init succeeded")
	}
	if err != context.DeadlineExceeded {
		t.Error("init didn't fail with DeadlineExceeded:", err)
	}
	cancel()
}

func TestSMSSend(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{
		"AT+CMGS=\"+123456789\"\r":            {"\n>"},
		"test message" + string(26):           {"\r\n", "+CMGS: 42\r\n", "\r\nOK\r\n"},
		"cruft test message" + string(26):     {"\r\n", "pad\r\n", "+CMGS: 43\r\n", "\r\nOK\r\n"},
		"malformed test message" + string(26): {"\r\n", "pad\r\n", "\r\nOK\r\n"},
	}
	g, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)

	ctx := context.Background()

	// OK
	mr, err := g.SendSMS(ctx, "+123456789", "test message")
	if err != nil {
		t.Error("send returned error", err)
	}
	if mr != "42" {
		t.Errorf("expected mr '42', but got '%s'", mr)
	}

	// ERROR
	mr, err = g.SendSMS(ctx, "+1234567890", "test message")
	if err == nil {
		t.Error("send succeeded")
	} else {
		if err.Error() != "ERROR" {
			t.Errorf("expected error 'ERROR', but got '%s'", err)
		}
	}
	if mr != "" {
		t.Errorf("expected mr '', but got '%s'", mr)
	}

	// extra cruft
	mr, err = g.SendSMS(ctx, "+123456789", "cruft test message")
	if err != nil {
		t.Error("send returned error", err)
	}
	if mr != "43" {
		t.Errorf("expected mr '43', but got '%s'", mr)
	}

	// malformed
	mr, err = g.SendSMS(ctx, "+123456789", "malformed test message")
	if err != ErrMalformedResponse {
		t.Error("send returned unexpected error", err)
	}
	if mr != "" {
		t.Errorf("expected mr '', but got '%s'", mr)
	}

	// cancelled
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	mr, err = g.SendSMS(cctx, "+123456789", "test message")
	if err == nil {
		t.Error("send succeeded")
	}
	if err != context.Canceled {
		t.Error("send didn't fail with canceled", err)
	}
	if mr != "" {
		t.Errorf("expected mr '', but got '%s'", mr)
	}

	// timeout
	cctx, cancel = context.WithTimeout(ctx, 0)
	mr, err = g.SendSMS(cctx, "+123456789", "test message")
	if err == nil {
		t.Error("send succeeded")
	}
	if err != context.DeadlineExceeded {
		t.Error("send didn't fail with canceled", err)
	}
	if mr != "" {
		t.Errorf("expected mr '', but got '%s'", mr)
	}
	cancel()
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
	if m.closeOnWrite {
		// provide time for commands to be queued before closing...
		time.Sleep(10 + time.Millisecond)
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
				// provide time for commands to be queued before closing...
				time.Sleep(10 + time.Millisecond)
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

func setupModem(t *testing.T, cmdSet map[string][]string) (*GSM, *mockModem) {
	mm := &mockModem{cmdSet: cmdSet, echo: true, r: make(chan []byte, 10)}
	var modem io.ReadWriter = mm
	debug := false // set to true to enable tracing of the flow to the mockModem.
	if debug {
		l := log.New(os.Stdout, "", log.LstdFlags)
		tr := trace.New(modem, l)
		//tr := trace.New(modem, l, trace.ReadFormat("r: %v"))
		modem = tr
	}
	g := New(modem)
	if g == nil {
		t.Fatal("new failed")
	}
	return g, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
