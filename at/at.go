// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package at provides a low level driver for AT modems.
package at

import (
	"bufio"
	"fmt"
	"io"
	"strings"
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
	//
	// Handled by the cmdLoop.
	cmdCh chan func()

	// channel for changes to inds
	//
	// Handled by the indLoop.
	indCh chan func()

	// closed when modem is closed
	closed chan struct{}

	// channel for all lines read from the modem
	//
	// Handled by the indLoop.
	iLines chan string

	// channel for lines read from the modem after indications removed
	//
	// Handled by the cmdLoop.
	cLines chan string

	// the underlying modem
	//
	// Only accessed from the cmdLoop.
	modem io.ReadWriter

	// the minimum time between an escape command and the subsequent command
	escTime time.Duration

	// time to wait for individual commands to complete
	cmdTimeout time.Duration

	// indications mapped by prefix
	//
	// Only accessed from the indLoop
	inds map[string]Indication

	// commands issued by Init.
	initCmds []string

	// if not-nil, the timer that must expire before the subsequent command is issued
	//
	// Only accessed from the cmdLoop.
	escGuard *time.Timer
}

// Option is a construction option for an AT.
type Option interface {
	applyOption(*AT)
}

// CommandOption defines a behaviouralk option for Command and SMSCommand.
type CommandOption interface {
	applyCommandOption(*commandConfig)
}

// InitOption defines a behaviouralk option for Init.
type InitOption interface {
	applyInitOption(*initConfig)
}

// New creates a new AT modem.
func New(modem io.ReadWriter, options ...Option) *AT {
	a := &AT{
		modem:      modem,
		cmdCh:      make(chan func()),
		indCh:      make(chan func()),
		iLines:     make(chan string),
		cLines:     make(chan string),
		closed:     make(chan struct{}),
		escTime:    20 * time.Millisecond,
		cmdTimeout: time.Second,
		inds:       make(map[string]Indication),
	}
	for _, option := range options {
		option.applyOption(a)
	}
	if a.initCmds == nil {
		a.initCmds = []string{
			"Z", // reset to factory defaults (also clears the escape from the rx buffer)
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
func WithEscTime(d time.Duration) EscTimeOption {
	return EscTimeOption(d)
}

// EscTimeOption defines the escape guard time for the modem.
type EscTimeOption time.Duration

func (o EscTimeOption) applyOption(a *AT) {
	a.escTime = time.Duration(o)
}

// InfoHandler receives indication info.
type InfoHandler func([]string)

// WithIndication adds an indication during construction.
func WithIndication(prefix string, handler InfoHandler, options ...IndicationOption) Indication {
	return newIndication(prefix, handler, options...)
}

func (o Indication) applyOption(a *AT) {
	a.inds[o.prefix] = o
}

// CmdsOption specifies the set of AT commands issued by Init.
type CmdsOption []string

func (o CmdsOption) applyOption(a *AT) {
	a.initCmds = []string(o)
}

func (o CmdsOption) applyInitOption(i *initConfig) {
	i.cmds = []string(o)
}

// WithCmds specifies the set of AT commands issued by Init.
//
// The default commands are ATZ.
func WithCmds(cmds ...string) CmdsOption {
	return CmdsOption(cmds)
}

// WithTimeout specifies the maximum time allowed for the modem to complete a
// command.
func WithTimeout(d time.Duration) TimeoutOption {
	return TimeoutOption(d)
}

// TimeoutOption specifies the maximum time allowed for the modem to complete a
// command.
type TimeoutOption time.Duration

func (o TimeoutOption) applyOption(a *AT) {
	a.cmdTimeout = time.Duration(o)
}

func (o TimeoutOption) applyInitOption(i *initConfig) {
	i.cmdOpts = append(i.cmdOpts, o)
}

func (o TimeoutOption) applyCommandOption(c *commandConfig) {
	c.timeout = time.Duration(o)
}

// AddIndication adds a handler for a set of lines beginning with the prefixed
// line and the following trailing lines.
func (a *AT) AddIndication(prefix string, handler InfoHandler, options ...IndicationOption) (err error) {
	ind := newIndication(prefix, handler, options...)
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
func (a *AT) Command(cmd string, options ...CommandOption) ([]string, error) {
	cfg := commandConfig{timeout: a.cmdTimeout}
	for _, option := range options {
		option.applyCommandOption(&cfg)
	}
	done := make(chan response)
	cmdf := func() {
		info, err := a.processReq(cmd, cfg.timeout)
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

// Escape issues an escape sequence to the modem.
//
// It does not wait for any response, but it does inhibit subsequent commands
// until the escTime has elapsed.
//
// The escape sequence is "\x1b\r\n".  Additional characters may be added to
// the sequence using the b parameter.
func (a *AT) Escape(b ...byte) {
	done := make(chan struct{})
	cmdf := func() {
		a.escape(b...)
		close(done)
	}
	select {
	case <-a.closed:
	case a.cmdCh <- cmdf:
		<-done
	}
}

// Init initialises the modem by escaping any outstanding SMS commands and
// resetting the modem to factory defaults.
//
// The Init is intended to be called after creation and before any other
// commands are issued in order to get the modem into a known state.  It can
// also be used subsequently to return the modem to a known state.
//
// The default init commands can be overridden by the options parameter.
func (a *AT) Init(options ...InitOption) error {
	// escape any outstanding SMS operations then CR to flush the command
	// buffer
	a.Escape([]byte("\r\n")...)

	cfg := initConfig{cmds: a.initCmds}
	for _, option := range options {
		option.applyInitOption(&cfg)
	}
	for _, cmd := range cfg.cmds {
		_, err := a.Command(cmd, cfg.cmdOpts...)
		switch err {
		case nil:
		case ErrDeadlineExceeded:
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
func (a *AT) SMSCommand(cmd string, sms string, options ...CommandOption) (info []string, err error) {
	cfg := commandConfig{timeout: a.cmdTimeout}
	for _, option := range options {
		option.applyCommandOption(&cfg)
	}
	done := make(chan response)
	cmdf := func() {
		info, err := a.processSmsReq(cmd, sms, cfg.timeout)
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

// issue an escape command
//
// This should only be called from within the cmdLoop.
func (a *AT) escape(b ...byte) {
	cmd := append([]byte(string(esc)+"\r\n"), b...)
	a.modem.Write(cmd)
	a.escGuard = time.NewTimer(a.escTime)
}

// perform a request  - issuing the command and awaiting the response.
func (a *AT) processReq(cmd string, timeout time.Duration) (info []string, err error) {
	a.waitEscGuard()
	err = a.writeCommand(cmd)
	if err != nil {
		return
	}

	cmdID := parseCmdID(cmd)
	var expChan <-chan time.Time
	if timeout >= 0 {
		expiry := time.NewTimer(timeout)
		expChan = expiry.C
		defer expiry.Stop()
	}
	for {
		select {
		case <-expChan:
			err = ErrDeadlineExceeded
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

// perform a SMS request  - issuing the command, awaiting the prompt, sending
// the data and awaiting the response.
func (a *AT) processSmsReq(cmd string, sms string, timeout time.Duration) (info []string, err error) {
	a.waitEscGuard()
	err = a.writeSMSCommand(cmd)
	if err != nil {
		return
	}
	cmdID := parseCmdID(cmd)
	var expChan <-chan time.Time
	if timeout >= 0 {
		expiry := time.NewTimer(timeout)
		expChan = expiry.C
		defer expiry.Stop()
	}
	for {
		select {
		case <-expChan:
			// cancel outstanding SMS request
			a.escape()
			err = ErrDeadlineExceeded
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

// waitEscGuard waits for a write guard to allow a write to the modem.
//
// This should only be called from within the cmdLoop.
func (a *AT) waitEscGuard() {
	if a.escGuard == nil {
		return
	}
Loop:
	for {
		select {
		case _, ok := <-a.cLines:
			if !ok {
				a.escGuard.Stop()
				break Loop
			}
		case <-a.escGuard.C:
			break Loop
		}
	}
	a.escGuard = nil
}

// writeCommand writes a one line command to the modem.
//
// This should only be called from within the cmdLoop.
func (a *AT) writeCommand(cmd string) error {
	cmdLine := "AT" + cmd + "\r\n"
	_, err := a.modem.Write([]byte(cmdLine))
	return err
}

// writeSMSCommand writes a the first line of an SMS command to the modem.
//
// This should only be called from within the cmdLoop.
func (a *AT) writeSMSCommand(cmd string) error {
	cmdLine := "AT" + cmd + "\r"
	_, err := a.modem.Write([]byte(cmdLine))
	return err
}

// writeSMS writes the first line of a two line SMS command to the modem.
//
// This should only be called from within the cmdLoop.
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

	// ErrDeadlineExceeded indicates the modem failed to complete an operation
	// within the required time.
	ErrDeadlineExceeded = errors.New("deadline exceeded")

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

// Indication represents an unsolicited result code (URC) from the modem, such
// as a received SMS message.
//
// Indications are lines prefixed with a particular pattern, and may include a
// number of trailing lines. The matching lines are bundled into a slice and
// sent to the handler.
type Indication struct {
	prefix  string
	lines   int
	handler InfoHandler
}

func newIndication(prefix string, handler InfoHandler, options ...IndicationOption) Indication {
	ind := Indication{
		prefix:  prefix,
		handler: handler,
		lines:   1,
	}
	for _, option := range options {
		option.applyIndicationOption(&ind)
	}
	return ind
}

// IndicationOption alters the behavior of the indication.
type IndicationOption interface {
	applyIndicationOption(*Indication)
}

// TrailingLinesOption specifies the number of trailing lines expected after an
// indication line.
type TrailingLinesOption int

func (o TrailingLinesOption) applyIndicationOption(ind *Indication) {
	ind.lines = int(o) + 1

}

// WithTrailingLines indicates the number of lines after the line containing
// the indication that arew to be collected as part of the indication.
//
// The default is 0 - only the indication line itself is collected and returned.
func WithTrailingLines(l int) TrailingLinesOption {
	return TrailingLinesOption(l)
}

// WithTrailingLine indicates the indication includes one line after the line
// containing the indication.
var WithTrailingLine = TrailingLinesOption(1)

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

type commandConfig struct {
	timeout time.Duration
}

type initConfig struct {
	timeout time.Duration
	cmds    []string
	cmdOpts []CommandOption
}
