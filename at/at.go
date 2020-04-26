// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package at provides a low level driver for AT modems.
package at

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// AT represents a modem that can be managed using AT commands.
//
// Commands can be issued to the modem using the Command and SMSCommand methods.
//
// The AT closes the closed channel when the connection to the underlying
// modem is broken (Read returns EOF).
//
// When closed, all outstanding commands return ErrClosed and the state of the
// underlying modem becomes unknown.
//
// Once closed the AT cannot be re-opened - it must be recreated.
type AT struct {
	// channel for commands issued to the modem
	cmdCh chan func()

	// channel for changes to inds
	indCh chan func()

	// closed when modem is closed
	closed chan struct{}

	// channel for all lines read from the modem
	iLines chan string

	// channel for lines read from the modem after indications removed
	cLines chan string

	// the underlying modem
	modem io.ReadWriter

	// the minimum time between an escape command and the subsequent command
	escTime time.Duration

	// indications mapped by prefix
	inds map[string]indication // only modified in nLoop

	// commands issued by Init.
	initCmds []string

	// covers escGuard
	escGuardMu sync.Mutex

	// if not-nil, the time the subsequent command must wait
	escGuard <-chan time.Time
}

// Option is a construction option for an AT.
type Option func(*AT)

// New creates a new AT modem.
func New(modem io.ReadWriter, options ...Option) *AT {
	a := &AT{
		modem:   modem,
		cmdCh:   make(chan func()),
		indCh:   make(chan func()),
		iLines:  make(chan string),
		cLines:  make(chan string),
		closed:  make(chan struct{}),
		escTime: 20 * time.Millisecond,
		inds:    make(map[string]indication),
	}
	for _, option := range options {
		option(a)
	}
	if a.initCmds == nil {
		a.initCmds = []string{
			"Z",       // reset to factory defaults (also clears the escape from the rx buffer)
			"^CURC=0", // disable general indications ^XXXX
		}
	}
	go lineReader(a.modem, a.iLines)
	go a.indLoop(a.indCh, a.iLines, a.cLines)
	go cmdLoop(a.cmdCh, a.cLines, a.closed)
	return a
}

const (
	sub = 0x1a
	esc = 0x1b
)

// WithEscTime sets the guard time for the modem.
//
// The escape time is the minimum time between an escape command being sent to
// the modem and any subsequent commands.
//
// The default guard time is 20msec.
func WithEscTime(d time.Duration) Option {
	return func(a *AT) {
		a.escTime = d
	}
}

// InfoHandler receives indication info.
type InfoHandler func([]string)

// WithIndication adds an indication during construction.
func WithIndication(prefix string, handler InfoHandler, options ...IndicationOption) Option {
	ind := indication{
		prefix:  prefix,
		handler: handler,
		lines:   1,
	}
	for _, option := range options {
		option(&ind)
	}
	return func(a *AT) {
		a.inds[prefix] = ind
	}
}

// WithInitCmds specifies the commands issued by Init.
//
// The default commands are ATZ and AT^CURC=0.
func WithInitCmds(cmds ...string) Option {
	return func(a *AT) {
		a.initCmds = cmds
	}
}

// Closed returns a channel which will block while the modem is not closed.
func (a *AT) Closed() <-chan struct{} {
	return a.closed
}

// Command issues the command to the modem and returns the result.
//
// The command should NOT include the AT prefix, nor <CR><LF> suffix which is
// automatically added.
//
// The return value includes the info (the lines returned by the modem between
// the command and the status line), or an error if the command did not
// complete successfully.
func (a *AT) Command(ctx context.Context, cmd string) ([]string, error) {
	done := make(chan response)
	cmdf := func() {
		info, err := a.processReq(ctx, cmd)
		done <- response{info: info, err: err}
	}
	select {
	case <-a.closed:
		return nil, ErrClosed
	case a.cmdCh <- cmdf:
		rsp := <-done
		return rsp.info, rsp.err
	}
}

// AddIndication adds a handler for a set of lines beginning with the prefixed
// line and the following trailing lines.
func (a *AT) AddIndication(prefix string, handler InfoHandler, options ...IndicationOption) (err error) {
	ind := indication{
		prefix:  prefix,
		handler: handler,
		lines:   1,
	}
	for _, option := range options {
		option(&ind)
	}
	errs := make(chan error)
	indf := func() {
		if _, ok := a.inds[ind.prefix]; ok {
			errs <- ErrIndicationExists
			return
		}
		a.inds[ind.prefix] = ind
		close(errs)
	}
	select {
	case <-a.closed:
		err = ErrClosed
	case a.indCh <- indf:
		err = <-errs
	}
	return
}

// CancelIndication removes any indication corresponding to the prefix.
//
// If any such indication exists its return channel is closed and no further
// indications will be sent to it.
func (a *AT) CancelIndication(prefix string) {
	done := make(chan struct{})
	indf := func() {
		delete(a.inds, prefix)
		close(done)
	}
	select {
	case <-a.closed:
	case a.indCh <- indf:
		<-done
	}
}

// Init initialises the modem by escaping any outstanding SMS commands
// and resetting the modem to factory defaults.
//
// The Init is intended to be called after creation and before any other commands
// are issued in order to get the modem into a known state.
//
// The default init commands can be overridden by the cmds parameter.
func (a *AT) Init(ctx context.Context, cmds ...string) error {
	// escape any outstanding SMS operations then CR to flush the command
	// buffer
	a.escape([]byte("\r\n")...)

	if cmds == nil {
		cmds = a.initCmds
	}
	for _, cmd := range cmds {
		_, err := a.Command(ctx, cmd)
		switch err {
		case nil:
		case context.DeadlineExceeded, context.Canceled:
			return err
		default:
			return fmt.Errorf("AT%s returned error: %w", cmd, err)
		}
	}
	return nil
}

// SMSCommand issues an SMS command to the modem, and returns the result.
//
// An SMS command is issued in two steps; first the command line:
//
//   AT<command><CR>
//
// which the modem responds to with a ">" prompt, after which the SMS PDU is
// sent to the modem:
//
//   <sms><Ctrl-Z>
//
// The modem then completes the command as per other commands, such as those
// issued by Command.
//
// The format of the sms may be a text message or a hex coded SMS PDU,
// depending on the configuration of the modem (text or PDU mode).
func (a *AT) SMSCommand(ctx context.Context, cmd string, sms string) (info []string, err error) {
	done := make(chan response)
	cmdf := func() {
		info, err := a.processSmsReq(ctx, cmd, sms)
		done <- response{info: info, err: err}
	}
	select {
	case <-a.closed:
		return nil, ErrClosed
	case a.cmdCh <- cmdf:
		rsp := <-done
		return rsp.info, rsp.err
	}
}

// cmdLoop is responsible for the interface to the modem.
//
// It serialises the issuing of commands and awaits the responses.
// If no command is pending then any lines received are discarded.
//
// The cmdLoop terminates when the downstream closes.
func cmdLoop(cmds chan func(), in <-chan string, out chan struct{}) {
	for {
		select {
		case cmd := <-cmds:
			cmd()
		case _, ok := <-in:
			if !ok {
				close(out)
				return
			}
		}
	}
}

// lineReader takes lines from m and redirects them to out.
//
// lineReader exits when m closes.
func lineReader(m io.Reader, out chan string) {
	scanner := bufio.NewScanner(m)
	scanner.Split(scanLines)
	for scanner.Scan() {
		out <- scanner.Text()
	}
	close(out) // tell pipeline we're done - end of pipeline will close the AT.
}

// indLoop is responsible for pulling indications from the stream of lines read
// from the modem, and forwarding them to handlers.
//
// Non-indication lines are passed upstream. Indication trailing lines are
// assumed to arrive in a contiguous block immediately after the indication.
//
// indLoop exits when the in channel closes.
func (a *AT) indLoop(cmds chan func(), in <-chan string, out chan string) {
	defer close(out)
	for {
		select {
		case cmd := <-cmds:
			cmd()
		case line, ok := <-in:
			if !ok {
				return
			}
			for prefix, ind := range a.inds {
				if strings.HasPrefix(line, prefix) {
					n := make([]string, ind.lines)
					n[0] = line
					for i := 1; i < ind.lines; i++ {
						t, ok := <-in
						if !ok {
							return
						}
						n[i] = t
					}
					ind.handler(n)
					continue
				}
			}
			out <- line
		}
	}
}

func (a *AT) processReq(ctx context.Context, cmd string) (info []string, err error) {
	a.waitEscGuard()
	err = a.writeCommand(cmd)
	if err != nil {
		return
	}
	cmdID := parseCmdID(cmd)
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		case line, ok := <-a.cLines:
			if !ok {
				return nil, ErrClosed
			}
			if line == "" {
				continue
			}
			lt := parseRxLine(line, cmdID)
			i, done, perr := a.processRxLine(lt, line)
			if i != nil {
				info = append(info, *i)
			}
			if perr != nil {
				err = perr
				return
			}
			if done {
				return
			}
		}
	}
}

func (a *AT) processSmsReq(ctx context.Context, cmd string, sms string) (info []string, err error) {
	a.waitEscGuard()
	err = a.writeSMSCommand(cmd)
	if err != nil {
		return
	}
	cmdID := parseCmdID(cmd)
	for {
		select {
		case <-ctx.Done():
			// cancel outstanding SMS request
			a.escape()
			err = ctx.Err()
			return
		case line, ok := <-a.cLines:
			if !ok {
				err = ErrClosed
				return
			}
			if line == "" {
				continue
			}
			lt := parseRxLine(line, cmdID)
			i, done, perr := a.processSmsRxLine(lt, line, sms)
			if i != nil {
				info = append(info, *i)
			}
			if perr != nil {
				err = perr
				return
			}
			if done {
				return
			}
		}
	}
}

// processRxLine parses a line received from the modem and determines how it
// adds to the response for the current command.
//
// The return values are:
//  - a line of info to be added to the response (optional)
//  - a flag indicating if the command is complete.
//  - an error detected while processing the command.
func (a *AT) processRxLine(lt rxl, line string) (info *string, done bool, err error) {
	switch lt {
	case rxlStatusOK:
		done = true
	case rxlStatusError:
		err = newError(line)
	case rxlUnknown, rxlInfo:
		info = &line
	case rxlConnect:
		info = &line
		done = true
	case rxlConnectError:
		err = ConnectError(line)
	}
	return
}

// processSmsRxLine parses a line received from the modem and determines how it
// adds to the response for the current command.
//
// The return values are:
//  - a line of info to be added to the response (optional)
//  - a flag indicating if the command is complete.
//  - an error detected while processing the command.
func (a *AT) processSmsRxLine(lt rxl, line string, sms string) (info *string, done bool, err error) {
	switch lt {
	case rxlUnknown:
		if line[len(line)-1] == sub && strings.HasPrefix(line, sms) {
			// swallow echoed SMS PDU
			return
		}
		info = &line
	case rxlSMSPrompt:
		if err = a.writeSMS(sms); err != nil {
			// escape SMS
			a.escape()
		}
	default:
		return a.processRxLine(lt, line)
	}
	return
}

// issue an escape command
func (a *AT) escape(b ...byte) {
	cmd := append([]byte(string(esc)+"\r\n"), b...)
	a.modem.Write(cmd)
	a.startEscGuard()
}

// startEscGuard starts a write guard that prevents a subsequent write within
// a short period of time (default 20ms).
func (a *AT) startEscGuard() {
	a.escGuardMu.Lock()
	a.escGuard = time.After(a.escTime)
	a.escGuardMu.Unlock()
}

// waitEscGuard waits for a write guard to allow a write to the modem.
func (a *AT) waitEscGuard() {
	a.escGuardMu.Lock()
	defer a.escGuardMu.Unlock()
	if a.escGuard == nil {
		return
	}
	for {
		select {
		case _, ok := <-a.cLines:
			if !ok {
				return
			}
		case <-a.escGuard:
			a.escGuard = nil
			return
		}
	}
}

// writeCommand writes a one line command to the modem.
func (a *AT) writeCommand(cmd string) error {
	cmdLine := "AT" + cmd + "\r\n"
	_, err := a.modem.Write([]byte(cmdLine))
	return err
}

// writeSMSCommand writes a the first line of an SMS command to the modem.
func (a *AT) writeSMSCommand(cmd string) error {
	cmdLine := "AT" + cmd + "\r"
	_, err := a.modem.Write([]byte(cmdLine))
	return err
}

// writeSMS writes the first line of a two line SMS command to the modem.
func (a *AT) writeSMS(sms string) error {
	_, err := a.modem.Write([]byte(sms + string(sub)))
	return err
}

// CMEError indicates a CME Error was returned by the modem.
//
// The value is the error value, in string form, which may be the numeric or
// textual, depending on the modem configuration.
type CMEError string

// CMSError indicates a CMS Error was returned by the modem.
//
// The value is the error value, in string form, which may be the numeric or
// textual, depending on the modem configuration.
type CMSError string

// ConnectError indicates an attempt to dial failed.
//
// The value of the error is the failure indication returned by the modem.
type ConnectError string

func (e CMEError) Error() string {
	return string("CME Error: " + e)
}

func (e CMSError) Error() string {
	return string("CMS Error: " + e)
}

func (e ConnectError) Error() string {
	return string("Connect: " + e)
}

var (
	// ErrClosed indicates an operation cannot be performed as the modem has
	// been closed.
	ErrClosed = errors.New("closed")

	// ErrError indicates the modem returned a generic AT ERROR in response to
	// an operation.
	ErrError = errors.New("ERROR")

	// ErrIndicationExists indicates there is already a indication registered
	// for a prefix.
	ErrIndicationExists = errors.New("indication exists")
)

// newError parses a line and creates an error corresponding to the content.
func newError(line string) error {
	var err error
	switch {
	case strings.HasPrefix(line, "ERROR"):
		err = ErrError
	case strings.HasPrefix(line, "+CMS ERROR:"):
		err = CMSError(strings.TrimSpace(line[11:]))
	case strings.HasPrefix(line, "+CME ERROR:"):
		err = CMEError(strings.TrimSpace(line[11:]))
	}
	return err
}

// response represents the result of a request operation performed on the
// modem.
//
// info is the collection of lines returned between the command and the status
// line. err corresponds to any error returned by the modem or while
// interacting with the modem.
type response struct {
	info []string
	err  error
}

// Received line types.
type rxl int

const (
	rxlUnknown rxl = iota
	rxlEchoCmdLine
	rxlInfo
	rxlStatusOK
	rxlStatusError
	rxlAsync
	rxlSMSPrompt
	rxlConnect
	rxlConnectError
)

// indication represents an unsolicited result code (URC) from the modem, such
// as a received SMS message.
//
// Indications are lines prefixed with a particular pattern, and may include a
// number of trailing lines. The matching lines are bundled into a slice and
// sent to the handler.
type indication struct {
	prefix  string
	lines   int
	handler InfoHandler
}

// IndicationOption alters the behavior of the indication.
type IndicationOption func(*indication)

// WithTrailingLines indicates the indication includes a number of lines after
// the line containing the indication.
func WithTrailingLines(l int) func(*indication) {
	return func(ind *indication) {
		ind.lines = l + 1
	}
}

// WithTrailingLine indicates the indication includes one line after the line
// containing the indication.
var WithTrailingLine = WithTrailingLines(1)

// parseCmdID returns the identifier component of the command.
//
// This is the section prior to any '=' or '?' and is generally, but not
// always, used to prefix info lines corresponding to the command.
func parseCmdID(cmdLine string) string {
	if idx := strings.IndexAny(cmdLine, "=?"); idx != -1 {
		return cmdLine[0:idx]
	}
	return cmdLine
}

// parseRxLine parses a received line and identifies the line type.
func parseRxLine(line string, cmdID string) rxl {
	switch {
	case line == "OK":
		return rxlStatusOK
	case strings.HasPrefix(line, "ERROR"),
		strings.HasPrefix(line, "+CME ERROR:"),
		strings.HasPrefix(line, "+CMS ERROR:"):
		return rxlStatusError
	case strings.HasPrefix(line, cmdID+":"):
		return rxlInfo
	case line == ">":
		return rxlSMSPrompt
	case strings.HasPrefix(line, "AT"+cmdID):
		return rxlEchoCmdLine
	case len(cmdID) == 0 || cmdID[0] != 'D':
		// Short circuit non-ATD commands.
		// No attempt to identify SMS PDUs at this level, so they will
		// be caught here, along with other unidentified lines.
		return rxlUnknown
	case strings.HasPrefix(line, "CONNECT"):
		return rxlConnect
	case line == "BUSY",
		line == "NO ANSWER",
		line == "NO CARRIER",
		line == "NO DIALTONE":
		return rxlConnectError
	default:
		// No attempt to identify SMS PDUs at this level, so they will
		// be caught here, along with other unidentified lines.
		return rxlUnknown
	}
}

// scanLines is a custom line scanner for lineReader that recognises the prompt
// returned by the modem in response to SMS commands such as +CMGS.
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// handle SMS prompt special case - no CR at prompt
	if len(data) >= 1 && data[0] == '>' {
		i := 1
		// there may be trailing space, so swallow that...
		for ; i < len(data) && data[i] == ' '; i++ {
		}
		return i, data[0:1], nil
	}
	return bufio.ScanLines(data, atEOF)
}
