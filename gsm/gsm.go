// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package gsm provides a driver for GSM modems.
package gsm

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/info"
	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/tpdu"
)

// GSM modem decorates the AT modem with GSM specific functionality.
type GSM struct {
	*at.AT
	sca     pdumode.SMSCAddress
	pduMode bool
	eOpts   []sms.EncoderOption
}

// Option is a construction option for the GSM.
type Option interface {
	applyOption(*GSM)
}

// RxOption is a construction option for the GSM.
type RxOption interface {
	applyRxOption(*rxConfig)
}

// New creates a new GSM modem.
func New(a *at.AT, options ...Option) *GSM {
	g := GSM{AT: a, pduMode: true}
	for _, option := range options {
		option.applyOption(&g)
	}
	return &g
}

type collectorOption struct {
	Collector
}

func (o collectorOption) applyRxOption(c *rxConfig) {
	c.c = Collector(o)
}

// WithCollector overrides the collector to be used to reassemble long messages.
//
// The default is an sms.Collector.
func WithCollector(c Collector) RxOption {
	return collectorOption{c}
}

type encoderOption struct {
	sms.EncoderOption
}

func (o encoderOption) applyOption(g *GSM) {
	g.eOpts = append(g.eOpts, o)
}

// WithEncoderOption applies the encoder option when converting from text
// messages to SMS TPDUs.
//
func WithEncoderOption(eo sms.EncoderOption) Option {
	return encoderOption{eo}
}

type pduModeOption bool

func (o pduModeOption) applyOption(g *GSM) {
	g.pduMode = bool(o)
}

// WithPDUMode specifies that the modem is to be used in PDU mode.
//
// This is the default mode.
var WithPDUMode = pduModeOption(true)

// WithTextMode specifies that the modem is to be used in text mode.
//
// This overrides is the default PDU mode.
var WithTextMode = pduModeOption(false)

type scaOption pdumode.SMSCAddress

// WithSCA sets the SCA used when transmitting SMSs in PDU mode.
//
// This overrides the default set in the SIM.
//
// The SCA is only relevant in PDU mode, so this option also enables PDU mode.
func WithSCA(sca pdumode.SMSCAddress) Option {
	return scaOption(sca)
}

func (o scaOption) applyOption(g *GSM) {
	g.pduMode = true
	g.sca = pdumode.SMSCAddress(o)
}

type timeoutOption time.Duration

func (o timeoutOption) applyRxOption(c *rxConfig) {
	c.timeout = time.Duration(o)
}

// WithReassemblyTimeout specifies the maximum time allowed for all segments in
// a long message to be received.
//
// The default is 24 hours.
//
// This option is overridden by WithCollector.
func WithReassemblyTimeout(d time.Duration) RxOption {
	return timeoutOption(d)
}

type cmdsOption []string

func (o cmdsOption) applyRxOption(c *rxConfig) {
	c.initCmds = []string(o)
}

// WithInitCmds overrides the commands required to setup the modem to notify when SMSs are received.
//
// The default is {"+CSMS=1","+CNMI=1,2,0,0,0"}
func WithInitCmds(c ...string) RxOption {
	return cmdsOption(c)
}

// Init initialises the GSM modem.
func (g *GSM) Init(options ...at.InitOption) (err error) {
	if err = g.AT.Init(options...); err != nil {
		return
	}
	// test GCAP response to ensure +GSM support, and modem sync.
	var i []string
	i, err = g.Command("+GCAP")
	if err != nil {
		return
	}
	capabilities := make(map[string]bool)
	for _, l := range i {
		if info.HasPrefix(l, "+GCAP") {
			caps := strings.Split(info.TrimPrefix(l, "+GCAP"), ",")
			for _, cap := range caps {
				capabilities[cap] = true
			}
		}
	}
	if !capabilities["+CGSM"] {
		return ErrNotGSMCapable
	}
	cmds := []string{
		"+CMGF=1", // text mode
		"+CMEE=2", // textual errors
	}
	if g.pduMode {
		cmds[0] = "+CMGF=0" // pdu mode
	}
	for _, cmd := range cmds {
		_, err = g.Command(cmd)
		if err != nil {
			return
		}
	}
	return
}

// SendShortMessage sends an SMS message to the number.
//
// If the modem is in PDU mode then the message is converted to a single SMS
// PDU.
//
// The mr is returned on success, else an error.
func (g *GSM) SendShortMessage(number string, message string, options ...at.CommandOption) (rsp string, err error) {
	if g.pduMode {
		var pdus []tpdu.TPDU
		eOpts := append(g.eOpts, sms.To(number))
		pdus, err = sms.Encode([]byte(message), eOpts...)
		if err != nil {
			return
		}
		if len(pdus) > 1 {
			err = ErrOverlength
			return
		}
		var tp []byte
		tp, err = pdus[0].MarshalBinary()
		if err != nil {
			return
		}
		return g.SendPDU(tp, options...)
	}
	var i []string
	i, err = g.SMSCommand("+CMGS=\""+number+"\"", message, options...)
	if err != nil {
		return
	}
	// parse response, ignoring any lines other than well-formed.
	for _, l := range i {
		if info.HasPrefix(l, "+CMGS") {
			rsp = info.TrimPrefix(l, "+CMGS")
			return
		}
	}
	err = ErrMalformedResponse
	return
}

// SendLongMessage sends an SMS message to the number.
//
// The modem must be in PDU mode.
// The message is split into concatenated SMS PDUs, if necessary.
//
// The mr of send PDUs is returned on success, else an error.
func (g *GSM) SendLongMessage(number string, message string, options ...at.CommandOption) (rsp []string, err error) {
	if !g.pduMode {
		err = ErrWrongMode
		return
	}
	var pdus []tpdu.TPDU
	eOpts := append(g.eOpts, sms.To(number))
	pdus, err = sms.Encode([]byte(message), eOpts...)
	if err != nil {
		return
	}
	for _, p := range pdus {
		var tp []byte
		tp, err = p.MarshalBinary()
		if err != nil {
			return
		}
		var mr string
		mr, err = g.SendPDU(tp, options...)
		if len(mr) > 0 {
			rsp = append(rsp, mr)
		}
		if err != nil {
			return
		}
	}
	return
}

// SendPDU sends an SMS PDU.
//
// tpdu is the binary TPDU to be sent.
// The mr is returned on success, else an error.
func (g *GSM) SendPDU(tpdu []byte, options ...at.CommandOption) (rsp string, err error) {
	if !g.pduMode {
		return "", ErrWrongMode
	}
	pdu := pdumode.PDU{SMSC: g.sca, TPDU: tpdu}
	var s string
	s, err = pdu.MarshalHexString()
	if err != nil {
		return
	}
	var i []string
	i, err = g.SMSCommand(fmt.Sprintf("+CMGS=%d", len(tpdu)), s, options...)
	if err != nil {
		return
	}
	// parse response, ignoring any lines other than well-formed.
	for _, l := range i {
		if info.HasPrefix(l, "+CMGS") {
			rsp = info.TrimPrefix(l, "+CMGS")
			return
		}
	}
	err = ErrMalformedResponse
	return
}

// Message encapsulates the details of a received message.
//
// The message is composed of one or more SMS-DELIVER TPDUs.
//
// Commonly required fields are extracted for easy access.
type Message struct {
	Number  string
	Message string
	SCTS    tpdu.Timestamp
	TPDUs   []*tpdu.TPDU
}

// MessageHandler receives a decoded SMS message from the modem.
type MessageHandler func(Message)

// ErrorHandler receives asynchronous errors.
type ErrorHandler func(error)

// Collector is the interface required to collect and reassemble TPDUs.
//
// By default this is implemented by an sms.Collector.
type Collector interface {
	Collect(tpdu.TPDU) ([]*tpdu.TPDU, error)
}

type rxConfig struct {
	timeout  time.Duration
	c        Collector
	initCmds []string
}

// StartMessageRx sets up the modem to receive SMS messages and pass them to
// the message handler.
//
// The message may have been concatenated over several SMS PDUs, but if so is
// reassembled into a complete message before being passed to the message
// handler.
//
// Errors detected while receiving messages are passed to the error handler.
//
// Requires the modem to be in PDU mode.
func (g *GSM) StartMessageRx(mh MessageHandler, eh ErrorHandler, options ...RxOption) error {
	if !g.pduMode {
		return ErrWrongMode
	}
	cfg := rxConfig{
		timeout:  24 * time.Hour,
		initCmds: []string{"+CSMS=1", "+CNMI=1,2,0,0,0"},
	}
	for _, option := range options {
		option.applyRxOption(&cfg)
	}
	if cfg.c == nil {
		rto := func(tpdus []*tpdu.TPDU) {
			eh(ErrReassemblyTimeout{tpdus})
		}
		cfg.c = sms.NewCollector(sms.WithReassemblyTimeout(cfg.timeout, rto))
	}
	cmtHandler := func(info []string) {
		tp, err := UnmarshalTPDU(info)
		if err != nil {
			eh(ErrUnmarshal{info, err})
			return
		}
		g.Command("+CNMA")
		tpdus, err := cfg.c.Collect(tp)
		if err != nil {
			eh(ErrCollect{tp, err})
			return
		}
		if tpdus == nil {
			return
		}
		m, err := sms.Decode(tpdus)
		if err != nil {
			eh(ErrDecode{tpdus, err})
		}
		if m != nil {
			mh(Message{
				Number:  tpdus[0].OA.Number(),
				Message: string(m),
				SCTS:    tpdus[0].SCTS,
				TPDUs:   tpdus,
			})
		}
	}
	err := g.AddIndication("+CMT:", cmtHandler, at.WithTrailingLine)
	if err != nil {
		return err
	}
	// tell the modem to forward SMS-DELIVERs via +CMT indications...
	for _, cmd := range cfg.initCmds {
		if _, err = g.Command(cmd); err != nil {
			g.CancelIndication("+CMT:")
			return err
		}
	}
	return nil
}

// StopMessageRx ends the reception of messages started by StartMessageRx,
func (g *GSM) StopMessageRx() {
	// tell the modem to stop forwarding SMSs to us.
	g.Command("+CNMI=0,0,0,0,0")
	// and detach the handler
	g.CancelIndication("+CMT:")
}

// UnmarshalTPDU converts +CMT info into the corresponding SMS TPDU.
func UnmarshalTPDU(info []string) (tp tpdu.TPDU, err error) {
	if len(info) < 2 {
		err = ErrUnderlength
		return
	}
	lstr := strings.Split(info[0], ",")
	var l int
	l, err = strconv.Atoi(lstr[len(lstr)-1])
	if err != nil {
		return
	}
	var pdu *pdumode.PDU
	pdu, err = pdumode.UnmarshalHexString(info[1])
	if err != nil {
		return
	}
	if int(l) != len(pdu.TPDU) {
		err = fmt.Errorf("length mismatch - expected %d, got %d", l, len(pdu.TPDU))
		return
	}
	err = tp.UnmarshalBinary(pdu.TPDU)
	return
}

// ErrCollect indicates that an error occured that prevented the TPDU from
// being collected.
type ErrCollect struct {
	TPDU tpdu.TPDU
	Err  error
}

func (e ErrCollect) Error() string {
	return fmt.Sprintf("error '%s' collecting TPDU: %+v", e.Err, e.TPDU)
}

// ErrDecode indicates that an error occured that prevented the TPDUs from
// being cdecoded.
type ErrDecode struct {
	TPDUs []*tpdu.TPDU
	Err   error
}

func (e ErrDecode) Error() string {
	str := fmt.Sprintf("error '%s' decoding: ", e.Err)
	for _, tpdu := range e.TPDUs {
		str += fmt.Sprintf("%+v", tpdu)
	}
	return str
}

// ErrReassemblyTimeout indicates that one or more segments of a long message
// are missing, preventing the complete message being reassembled.
//
// The missing segments are the nil entries in the array.
type ErrReassemblyTimeout struct {
	TPDUs []*tpdu.TPDU
}

func (e ErrReassemblyTimeout) Error() string {
	str := "timeout reassembling: "
	for _, tpdu := range e.TPDUs {
		str += fmt.Sprintf("%+v", tpdu)
	}
	return str
}

// ErrUnmarshal indicates an error occured while trying to unmarshal the TPDU
// received from the modem.
type ErrUnmarshal struct {
	Info []string
	Err  error
}

func (e ErrUnmarshal) Error() string {
	str := fmt.Sprintf("error '%s' unmarshalling: ", e.Err)
	for _, i := range e.Info {
		str += fmt.Sprintf("%s\n", i)
	}
	return str
}

var (
	// ErrMalformedResponse indicates the modem returned a badly formed
	// response.
	ErrMalformedResponse = errors.New("modem returned malformed response")

	// ErrNotGSMCapable indicates that the modem does not support the GSM
	// command set, as determined from the GCAP response.
	ErrNotGSMCapable = errors.New("modem is not GSM capable")

	// ErrNotPINReady indicates the modem SIM card is not ready to perform
	// operations.
	ErrNotPINReady = errors.New("modem is not PIN Ready")

	// ErrOverlength indicates the message is too long for a single PDU and
	// must be split into multiple PDUs.
	ErrOverlength = errors.New("message too long for one SMS")

	// ErrUnderlength indicates that two few lines of info were provided to
	// decode a PDU.
	ErrUnderlength = errors.New("insufficient info")

	// ErrWrongMode indicates the GSM modem is operating in the wrong mode and
	// so cannot support the command.
	ErrWrongMode = errors.New("modem is in the wrong mode")
)
