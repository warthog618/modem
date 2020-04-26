// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package gsm provides a driver for GSM modems.
package gsm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/info"
	"github.com/warthog618/sms/encoding/pdumode"
)

// GSM modem decorates the AT modem with GSM specific functionality.
type GSM struct {
	*at.AT
	sca     pdumode.SMSCAddress
	pduMode bool
}

// Option is a construction option for the GSM.
type Option interface {
	applyOption(*GSM)
}

// New creates a new GSM modem.
func New(a *at.AT, options ...Option) *GSM {
	g := GSM{AT: a}
	for _, option := range options {
		option.applyOption(&g)
	}
	return &g
}

type PDUModeOption bool

func (o PDUModeOption) applyOption(g *GSM) {
	g.pduMode = bool(o)
}

// WithPDUMode specifies that the modem is to be used in PDU mode.
//
// The default is text mode.
var WithPDUMode = PDUModeOption(true)

type SCAOption pdumode.SMSCAddress

// WithSCA sets the SCA used when transmitting SMSs.
//
// This overrides the default set in the SIM.
//
// The SCA is only relevant in PDU mode, so this option also enables PDU mode.
func WithSCA(sca pdumode.SMSCAddress) SCAOption {
	return SCAOption(sca)
}

func (o SCAOption) applyOption(g *GSM) {
	g.pduMode = true
	g.sca = pdumode.SMSCAddress(o)
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

// SendSMS sends an SMS message to the number.
//
// The mr is returned on success, else an error.
func (g *GSM) SendSMS(number string, message string, options ...at.CommandOption) (rsp string, err error) {
	if g.pduMode {
		err = ErrWrongMode
		return
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

// SendSMSPDU sends an SMS PDU.
//
// tpdu is the binary TPDU to be sent.
// The mr is returned on success, else an error.
func (g *GSM) SendSMSPDU(tpdu []byte, options ...at.CommandOption) (rsp string, err error) {
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

var (
	// ErrNotGSMCapable indicates that the modem does not support the GSM
	// command set, as determined from the GCAP response.
	ErrNotGSMCapable = errors.New("modem is not GSM capable")

	// ErrNotPINReady indicates the modem SIM card is not ready to perform operations.
	ErrNotPINReady = errors.New("modem is not PIN Ready")

	// ErrMalformedResponse indicates the modem returned a badly formed
	// response.
	ErrMalformedResponse = errors.New("modem returned malformed response")

	// ErrWrongMode indicates the GSM modem is operating in the wrong mode and so cannot support the command.
	ErrWrongMode = errors.New("modem is in the wrong mode")
)
