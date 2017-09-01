package at

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// AT represents a modem that can be managed using AT commands.
// Commands can be issued to the modem using the Command and SMSCommand methods.
// The AT closes the closed channel when the connection to the underlying
// modem is broken (Read returns EOF) .
// When closed, all outstanding commands return ErrClosed and the state of the
// underlying modem becomes unknown.
// Once closed the AT cannot be re-opened - it must be recreated.
type AT struct {
	cmdCh  chan func()
	indCh  chan func()
	closed chan struct{}
	iLines chan string
	cLines chan string
	modem  io.ReadWriter
	n      map[string]indication // only modified in nLoop
}

// New creates a new AT modem.
func New(modem io.ReadWriter) *AT {
	a := &AT{
		modem:  modem,
		cmdCh:  make(chan func()),
		indCh:  make(chan func()),
		iLines: make(chan string),
		cLines: make(chan string),
		closed: make(chan struct{}),
		n:      make(map[string]indication),
	}
	go lineReader(a.modem, a.iLines)
	go a.nLoop(a.indCh, a.iLines, a.cLines)
	go cmdLoop(a.cmdCh, a.cLines, a.closed)
	return a
}

// Closed returns a channel which will block while the modem is not closed.
func (a *AT) Closed() <-chan struct{} {
	return a.closed
}

// Command issues the command to the modem and returns the result.
// The command should NOT include the AT prefix, or \r\n suffix which is automatically added.
// The return value includes the info (the lines returned by the modem between the command and
// the status line), and an error which is non-nil if the command did not complete successfully.
func (a *AT) Command(ctx context.Context, cmd string) ([]string, error) {
	done := make(chan response)
	select {
	case <-a.closed:
		return nil, ErrClosed
	case a.cmdCh <- func() {
		done <- a.processReq(ctx, request{cmd: cmd})
	}:
		rsp := <-done
		return rsp.info, rsp.err
	}
}

// AddIndication adds a handler for a set of lines begining with the prefixed
// line and the following trailing lines.
// Each set of lines is returned via the returned channel.
// The return channel is closed when the AT closes.
func (a *AT) AddIndication(prefix string, trailingLines int) (<-chan []string, error) {
	done := make(chan chan []string)
	errs := make(chan error)
	select {
	case <-a.closed:
		return nil, ErrClosed
	case a.indCh <- func() {
		if _, ok := a.n[prefix]; ok {
			errs <- ErrIndicationExists
			return
		}
		n := indication{prefix, trailingLines + 1, make(chan []string)}
		a.n[prefix] = n
		done <- n.c
	}:
		select {
		case evtCh := <-done:
			return evtCh, nil
		case err := <-errs:
			return nil, err
		}
	}
}

// CancelIndication removes any indication corresponding to the prefix.
// If any such indication exists its return channel is closed and no further
// indications will be sent to it.
func (a *AT) CancelIndication(prefix string) {
	done := make(chan struct{})
	select {
	case <-a.closed:
		return
	case a.indCh <- func() {
		n, ok := a.n[prefix]
		if ok {
			close(n.c)
			delete(a.n, prefix)
		}
		close(done)
	}:
		<-done
	}
}

// Init initialises the modem by escaping any outstanding SMS commands
// and resetting the modem to factory defaults.
// This is a bare minimum init.
func (a *AT) Init(ctx context.Context) error {
	// escape any outstanding SMS operations then CR to clear the command buffer
	a.modem.Write([]byte(string(27) + "\r\n\r\n"))
	// allow time for response, or at least any residual OK, to propagate and be disacarded.
	<-time.After(20 * time.Millisecond)
	cmds := []string{
		"Z",       // reset to factory defaults (also clears the escape from the rx buffer)
		"^CURC=0", // disable general indications ^XXXX
	}
	for _, cmd := range cmds {
		_, err := a.Command(ctx, cmd)
		switch err {
		case nil:
		case context.DeadlineExceeded, context.Canceled:
			return err
		default:
			return fmt.Errorf("AT%s returned error %v", cmd, err)
		}
	}
	return nil
}

// SMSCommand issues an SMS command to the modem, and returns the result.
// An SMS command is issued in two steps; first the command line:
//   AT<command>\r
// which the modem responds to with a ">" prompt, after which the SMS PDU is sent to the modem:
//   <sms><Ctrl-Z>
// The modem then completes the command as per other commands, such as those issued by Command.
// The format of the sms may be a text message or a hex coded SMS PDU, depending on the
// configuration of the modem (text or PDU mode).
func (a *AT) SMSCommand(ctx context.Context, cmd string, sms string) ([]string, error) {
	done := make(chan response)
	select {
	case <-a.closed:
		return nil, ErrClosed
	case a.cmdCh <- func() {
		done <- a.processReq(ctx, request{cmd: cmd, sms: &sms})
	}:
		rsp := <-done
		return rsp.info, rsp.err
	}
}

// cmdLoop is responsible for the interface to the modem.
// It serialises the issuing of commands and awaits the responses.
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

func lineReader(m io.Reader, out chan string) {
	scanner := bufio.NewScanner(m)
	scanner.Split(scanLines)
	for scanner.Scan() {
		select {
		case out <- scanner.Text():
			break
		}
	}
	close(out) // tell pipeline we're done - end of pipeline will close the AT.
}

// nLoop is responsible for pulling indications from the stream of lines read from the modem,
// and forwarding them to handlers.  Non-indication lines are passed upstream.
// Indication trailing lines are assumed to arrive in a contiguous
// block immediately after the indication.
func (a *AT) nLoop(cmds chan func(), in <-chan string, out chan string) {
Loop:
	for {
		select {
		case cmd := <-cmds:
			cmd()
		case line, ok := <-in:
			if !ok {
				close(out)
				break Loop
			}
			for k, v := range a.n {
				if strings.HasPrefix(line, k) {
					n := make([]string, v.totalLines)
					n[0] = line
					for i := 1; i < v.totalLines; i++ {
						t, ok := <-in
						if !ok {
							break Loop
						}
						n[i] = t
					}
					v.c <- n
					continue
				}
			}
			out <- line
		}
	}
	for k, v := range a.n {
		close(v.c)
		delete(a.n, k)
	}
}

func (a *AT) processReq(ctx context.Context, req request) response {
	if err := a.writeCommand(req); err != nil {
		return response{err: err}
	}
	cmdID := parseCmdID(req.cmd)
	var rsp response
	for {
		select {
		case <-ctx.Done():
			rsp.err = ctx.Err()
			return rsp
		case line, ok := <-a.cLines:
			if !ok {
				return response{err: ErrClosed}
			}
			if line == "" {
				continue
			}
			switch parseRxLine(line, cmdID) {
			case rxlStatusOK:
				return rsp
			case rxlStatusError:
				rsp.err = newError(line)
				return rsp
			case rxlUnknown:
				if req.sms != nil && line[len(line)-1] == 26 && strings.HasPrefix(line, *req.sms) {
					// swallow echoed SMS PDU
					continue
				}
				fallthrough
			case rxlInfo:
				if rsp.info == nil {
					rsp.info = make([]string, 0)
				}
				rsp.info = append(rsp.info, line)
			case rxlSMSPrompt:
				if req.sms != nil {
					if err := a.writeSMS(*req.sms); err != nil {
						return response{err: err}
					}
				}
			}
		}
	}
}

// writeCommand writes a one line command to the modem.
func (a *AT) writeCommand(req request) error {
	cmdLine := "AT" + req.cmd + "\r\n"
	if req.sms != nil {
		cmdLine = cmdLine[:len(cmdLine)-1]
	}
	_, err := a.modem.Write([]byte(cmdLine))
	return err
}

// writeSMS writes the first line of a two line SMS command to the modem.
func (a *AT) writeSMS(sms string) error {
	_, err := a.modem.Write([]byte(sms + string(26)))
	return err
}

// CMEError indicates a CME Error was returned by the modem.
// The value is the error value, in string form, which may be the numeric or textual, depending
// on the modem configuration.
type CMEError string

// CMSError indicates a CMS Error was returned by the modem.
// The value is the error value, in string form, which may be the numeric or textual, depending
// on the modem configuration.
type CMSError string

func (e CMEError) Error() string {
	return string("CME Error: " + e)
}

func (e CMSError) Error() string {
	return string("CMS Error: " + e)
}

var (
	// ErrClosed indicates an operation cannot be performed as the modem has been closed.
	ErrClosed = errors.New("closed")
	// ErrError indicates the modem returned a generic AT ERROR in response to an operation.
	ErrError = errors.New("ERROR")
	// ErrIndicationExists indicates there is already a indication registered for
	// a prefix.
	ErrIndicationExists = errors.New("indication exists")
)

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

// request represents an operation to be performed on the modem.
type request struct {
	cmd string
	sms *string
}

// response represents the result of a request operation performed on the modem.
// info is the collection of lines returned between the command and the status line.
// err corresponds to any error returned by the modem or while interacting with the modem.
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
)

// indication represents an unsolicited result code (URC) from the modem, such as a
// received SMS message.
// Indications are lines prefixed with a particular pattern,
// and may include a number of trailing lines.
// The matching lines are bundled into a slice and injected into the channel.
type indication struct {
	prefix     string
	totalLines int
	c          chan []string
}

// parseCmdID returns the identifier component of the command.
// This is the section prior to any '=' or '?' and is generally, but not
// always, used to prefix info lines corresponding to the command.
func parseCmdID(cmdLine string) string {
	switch idx := strings.IndexAny(cmdLine, "=?"); idx {
	case -1:
		return cmdLine
	default:
		return cmdLine[0:idx]
	}
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
	default:
		// Don't attempt to identify SMS PDUs at this level, so they will
		// be caught here, along with other unidentified lines.
		return rxlUnknown
	}
}

// scanLines is a custom line scanner for lineReader that recognises
// the prompt returned by the modem in response to SMS commands such as +CMGS.
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
