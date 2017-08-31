/*
  Test suite for AT module.

	Note that these tests provide a mockModem which does not attempt to emulate
	a serial modem, but which provides responses required to exercise at.go
	So, while the commands may follow the structure of the AT protocol they
	most certainly are not AT commands - just patterns that elicit the behaviour
	required for the test.
*/
package at

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/warthog618/modem/trace"
)

func TestNew(t *testing.T) {
	// mocked
	mm := mockModem{cmdSet: nil, echo: false, r: make(chan []byte, 10)}
	defer teardownModem(&mm)
	a := New(&mm)
	if a == nil {
		t.Fatal("New failed")
	}
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
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
	a := New(&mm)
	if a == nil {
		t.Fatal("New failed")
	}
	ctx := context.Background()
	err := a.Init(ctx)
	if err != nil {
		t.Fatal("Init failed", err)
	}
	select {
	case <-a.Closed():
		t.Error("modem closed")
	default:
	}
	// residual OKs
	mm.r <- []byte("\r\nOK\r\nOK\r\n")
	err = a.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}
	// residual ERRORs
	mm.r <- []byte("\r\nERROR\r\nERROR\r\n")
	err = a.Init(ctx)
	if err != nil {
		t.Error("init failed", err)
	}

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
	a := New(&mm)
	if a == nil {
		t.Fatal("New failed")
	}
	ctx := context.Background()
	err := a.Init(ctx)
	if err == nil {
		t.Fatal("New succeeded")
	}
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
	a := New(&mm)
	if a == nil {
		t.Error("returned nil modem")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := a.Init(ctx)
	if err != context.DeadlineExceeded {
		t.Error("init failed to timeout", err)
	}
}

func TestCommand(t *testing.T) {
	cmdSet := map[string][]string{
		"AT\r\n":       {"OK\r\n"},
		"ATPASS\r\n":   {"OK\r\n"},
		"ATINFO=1\r\n": {"info1\r\n", "info2\r\n", "INFO: info3\r\n", "\r\n", "OK\r\n"},
		"ATCMS\r\n":    {"+CMS ERROR: 204\r\n"},
		"ATCME\r\n":    {"+CME ERROR: 42\r\n"},
	}
	m, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)
	ctx := context.Background()
	// empty req (AT)
	info, err := m.Command(ctx, "")
	if err != nil {
		t.Error("Empty command failed")
	}
	if info != nil {
		t.Error("Empty command returned info:", info)
	}

	// non-empty request
	info, err = m.Command(ctx, "PASS")
	if err != nil {
		t.Error("Non-empty command failed")
	}
	if info != nil {
		t.Error("Non-empty command returned info:", info)
	}

	// non-empty info
	info, err = m.Command(ctx, "INFO=1")
	if err != nil {
		t.Error("Info command failed")
	}
	if info == nil {
		t.Error("Info command didn't return info")
	}

	// ERROR
	info, err = m.Command(ctx, "ERR")
	if err != ErrError {
		t.Error("Error command didn't error")
	}
	if info != nil {
		t.Error("Error command returned info:", info)
	}

	// CMS Error
	info, err = m.Command(ctx, "CMS")
	if err != nil {
		cms, ok := err.(CMSError)
		if !ok {
			t.Error("CMSError command didn't error")
		}
		if cms != "204" {
			t.Error("CMSError command didn't error expected value. got '" + cms + "' expected '204'")
		}
		if cms.Error() != "CMS Error: 204" {
			t.Error("CMSError not formatted as expected - got ", cms)
		}
	}
	if info != nil {
		t.Error("CMSError command returned info:", info)
	}

	// CME Error
	info, err = m.Command(ctx, "CME")
	if err != nil {
		cme, ok := err.(CMEError)
		if !ok {
			t.Error("CMEError command didn't error")
		}
		if cme != "42" {
			t.Error("CMEError command didn't error expected value. got '" + cme + "' expected '42'")
		}
		if cme.Error() != "CME Error: 42" {
			t.Error("CMEError not formatted as expected - got ", cme)
		}
	}
	if info != nil {
		t.Error("CMSError command returned info:", info)
	}

	// no echo
	mm.echo = false
	// no-echo non-empty info
	info, err = m.Command(ctx, "INFO=1")
	if err != nil {
		t.Error("Info command failed")
	}
	if info == nil {
		t.Error("Info command didn't return info")
	}

	// closed before response
	mm.closeOnWrite = true
	info, err = m.Command(ctx, "NULL")
	if err == nil {
		t.Error("closed before response didn't return error")
	}
	if info != nil {
		t.Error("closed before response returned info:", info)
	}

	// closed before request
	info, err = m.Command(ctx, "PASS")
	if err == nil {
		t.Error("closed before request didn't return error")
	}
	if info != nil {
		t.Error("closed before request returned info:", info)
	}

	// write Error
	mm.errOnWrite = true
	info, err = m.Command(ctx, "PASS")
	if err == nil {
		t.Error("Write error command didn't return error")
	}
	if info != nil {
		t.Error("Write error command returned info:", info)
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
	done := make(chan struct{})
	ctx := context.Background()
	go func() {
		info, err := m.Command(ctx, "PASS")
		if err == nil {
			t.Error("closed before request didn't return error")
		}
		if info != nil {
			t.Error("closed before request returned info:", info)
		}
		close(done)
	}()
	// closed before request
	info, err := m.Command(ctx, "PASS")
	if err == nil {
		t.Error("closed before request didn't return error")
	}
	if info != nil {
		t.Error("closed before request returned info:", info)
	}
	<-done
}

func TestCommandClosedPreWrite(t *testing.T) {
	// retest this case separately to catch closure on the write to modem.
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)
	mm.Close()
	ctx := context.Background()
	// closed before request
	info, err := m.Command(ctx, "PASS")
	if err == nil {
		t.Error("closed before request didn't return error")
	}
	if info != nil {
		t.Error("closed before request returned info:", info)
	}
}

func checkInfo(expected []string, received []string) error {
	if len(expected) != len(received) {
		return fmt.Errorf("inconsistent lengths - expected %d, got %d", len(expected), len(received))
	}
	for idx, v := range expected {
		x := strings.TrimRight(v, "\r\n")
		if received[idx] != x {
			return fmt.Errorf("inconsistent line - expected %s, got %s", x, received[idx])
		}
	}
	return nil
}

func TestSMSCommand(t *testing.T) {
	cmdSet := map[string][]string{
		"ATCMS\r":           {"\r\n+CMS ERROR: 204\r\n"},
		"ATCME\r":           {"\r\n+CME ERROR: 42\r\n"},
		"ATSMS\r":           {"\n>"},
		"ATSMS2\r":          {"\n> "},
		"info" + string(26): {"info1\r\n", "info2\r\n", "INFO: info3\r\n", "\r\n", "OK\r\n"},
		"sms+" + string(26): {"\r\n", "info1\r\n", "info2\r\n", "INFO: info3\r\n", "\r\n", "OK\r\n"},
	}
	m, mm := setupModem(t, cmdSet)
	defer teardownModem(mm)
	ctx := context.Background()

	// Error
	info, err := m.SMSCommand(ctx, "ERR", "errsms")
	if err != ErrError {
		t.Error("Error command didn't error")
	}
	if info != nil {
		t.Error("Error command returned info:", info)
	}

	// CMS Error
	info, err = m.SMSCommand(ctx, "CMS", "cmssms")
	if err != nil {
		cms, ok := err.(CMSError)
		if !ok {
			t.Error("CMSError command didn't error")
		}
		if cms != "204" {
			t.Error("CMSError command didn't error expected value. got '" + cms + "' expected '204'")
		}
	}
	if info != nil {
		t.Error("CMSError command returned info:", info)
	}

	// CME Error
	info, err = m.SMSCommand(ctx, "CME", "cmesms")
	if err != nil {
		cme, ok := err.(CMEError)
		if !ok {
			t.Error("CMEError command didn't error")
		}
		if cme != "42" {
			t.Error("CMEError command didn't error expected value. got '" + cme + "' expected '42'")
		}
	}
	if info != nil {
		t.Error("CMSError command returned info:", info)
	}
	// OK
	info, err = m.SMSCommand(ctx, "SMS", "sms+")
	if err != nil {
		t.Error("SMS command failed")
	}
	if info == nil {
		t.Error("SMS command returned nil info")
	} else {
		if err = checkInfo(cmdSet["sms+"+string(26)][1:len(cmdSet["sms+"+string(26)])-2], info); err != nil {
			t.Error(err)
		}
	}

	// No echo
	mm.echo = false
	info, err = m.SMSCommand(ctx, "SMS2", "info")
	if err != nil {
		t.Error("SMS command failed")
	}
	if info == nil {
		t.Error("SMS command returned info:", info)
	} else {
		if err = checkInfo(cmdSet["info"+string(26)][0:len(cmdSet["info"+string(26)])-2], info); err != nil {
			t.Error(err)
		}
	}

	// write error
	mm.errOnWrite = true
	info, err = m.SMSCommand(ctx, "EoW", "errOnWrite")
	if err == nil {
		t.Error("SMS command didn't return write error")
	}
	if info != nil {
		t.Error("SMS command returned info:", info)
	}

	// closed before response
	mm.closeOnWrite = true
	info, err = m.SMSCommand(ctx, "CoW", "closeOnWrite")
	if err == nil {
		t.Error("SMS command didn't return write error")
	}
	if info != nil {
		t.Error("SMS command returned info:", info)
	}

	// closed before request
	info, err = m.SMSCommand(ctx, "C", "closed")
	if err == nil {
		t.Error("SMS command didn't return write error")
	}
	if info != nil {
		t.Error("SMS command returned info:", info)
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
	ctx := context.Background()
	done := make(chan struct{})
	// Need to queue multiple commands to check queued commands code path.
	go func() {
		info, err := m.SMSCommand(ctx, "SMS", "closed")
		if err == nil {
			t.Error("SMS command didn't return write error")
		}
		if info != nil {
			t.Error("SMS command returned info:", info)
		}
		close(done)
	}()
	info, err := m.SMSCommand(ctx, "SMS", "closed")
	if err == nil {
		t.Error("SMS command didn't return write error")
	}
	if info != nil {
		t.Error("SMS command returned info:", info)
	}
	<-done
}

func TestAddIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c, err := m.AddIndication("notify", 0)
	if err != nil {
		t.Errorf("returned unexected error %v", err)
	}
	if c == nil {
		t.Fatalf("didn't return channel")
	}
	select {
	case n := <-c:
		t.Errorf("got notification without write: %v", n)
	default:
	}
	mm.r <- []byte("notify: :yfiton\r\n")
	select {
	case n := <-c:
		if err = checkInfo(n, []string{"notify: :yfiton"}); err != nil {
			t.Error(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("no notification received")
	}
	c2, err := m.AddIndication("notify", 0)
	if err == nil {
		t.Error("failed to prevent re-adding")
	}
	if c2 != nil {
		t.Errorf("returned channel on error")
	}
	c2, err = m.AddIndication("foo", 2)
	if err != nil {
		t.Errorf("returned unexected error %v", err)
	}
	if c2 == nil {
		t.Fatalf("didn't return channel")
	}
	mm.r <- []byte("foo:\r\nbar\r\nbaz\r\n")
	select {
	case n := <-c2:
		if err = checkInfo(n, []string{"foo:", "bar", "baz"}); err != nil {
			t.Error(err)
		}
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
	if err == nil {
		t.Errorf("allowed add while closed")
	}
	if c2 != nil {
		t.Errorf("returned channel on error")
	}

}

func TestCancelIndication(t *testing.T) {
	m, mm := setupModem(t, nil)
	defer teardownModem(mm)

	c, err := m.AddIndication("notify", 0)
	if err != nil {
		t.Errorf("returned unexected error %v", err)
	}
	if c == nil {
		t.Fatalf("didn't return channel")
	}
	c2, err := m.AddIndication("foo", 2)
	if err != nil {
		t.Errorf("returned unexected error %v", err)
	}
	if c2 == nil {
		t.Fatalf("didn't return channel")
	}
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
	if err != nil {
		t.Errorf("returned unexected error %v", err)
	}
	if c == nil {
		t.Fatalf("didn't return channel")
	}
	mm.r <- []byte("foo:\r\nbar\r\n")
	mm.Close()
	select {
	case <-c:
	case <-time.After(100 * time.Millisecond):
		t.Error("channel 2 still open")
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

func setupModem(t *testing.T, cmdSet map[string][]string) (*AT, *mockModem) {
	mm := &mockModem{cmdSet: cmdSet, echo: true, r: make(chan []byte, 10)}
	var modem io.ReadWriter = mm
	debug := false // set to true to enable tracing of the flow to the mockModem.
	if debug {
		l := log.New(os.Stdout, "", log.LstdFlags)
		//tr := trace.New(modem, l)
		tr := trace.New(modem, l, trace.ReadFormat("r: %v"))
		modem = tr
	}
	a := New(modem)
	if a == nil {
		t.Fatal("new failed")
	}
	return a, mm
}

func teardownModem(m *mockModem) {
	m.Close()
}
