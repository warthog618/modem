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
	"testing"
	"time"
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

}

func TestSMSSend(t *testing.T) {
	// mocked
	cmdSet := map[string][]string{}
	mm := mockModem{cmdSet: cmdSet, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	g := New(&mm)
	if g == nil {
		t.Fatal("New failed")
	}

	// OK

	// ERROR

	// bad length
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

func teardownModem(m *mockModem) {
	m.Close()
}
