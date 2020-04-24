// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package gsm provides a driver for GSM modems.
package gsm

import (
	"context"
	"errors"
	"fmt"
	"io"
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
type Option func(*GSM)

// New creates a new GSM modem.
func New(options ...Option) *GSM {
	g := GSM{}
	for _, option := range options {
		option(&g)
	}
	if g.AT == nil {
		return nil
	}
	return &g
}

// FromReadWriter specifies a ReadWriter that will be wrapped in a generic at.AT.
//
// If you require a customised at.AT then use FromAT instead.
func FromReadWriter(rw io.ReadWriter) Option {
	return func(g *GSM) {
		g.AT = at.New(rw)
	}
}

// FromAT specifies an explicit AT that the GSM should wrap.
func FromAT(a *at.AT) Option {
	return func(g *GSM) {
		g.AT = a
	}
}

// WithPDUMode specifies that the modem is to be used in PDU mode.
//
// The default is text mode.
var WithPDUMode = func(g *GSM) {
	g.pduMode = true
}

// WithSCA sets the SCA used when transmitting SMSs.
//
// This overrides the default set in the SIM.
//
// The SCA is only relevant in PDU mode, so this option also enables PDU mode.
func WithSCA(sca pdumode.SMSCAddress) Option {
	return func(g *GSM) {
		g.pduMode = true
		g.sca = sca
	}
}

// Init initialises the GSM modem.
func (g *GSM) Init(ctx context.Context, initCmds ...string) (err error) {
	if err = g.AT.Init(ctx, initCmds...); err != nil {
		return
	}
	// test GCAP response to ensure +GSM support, and modem sync.
	var i []string
	i, err = g.Command(ctx, "+GCAP")
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
		_, err = g.Command(ctx, cmd)
		if err != nil {
			return
		}
	}
	return
}

// SendSMS sends an SMS message to the number.
//
// The mr is returned on success, else an error.
func (g *GSM) SendSMS(ctx context.Context, number string, message string) (rsp string, err error) {
	if g.pduMode {
		err = ErrWrongMode
		return
	}
	var i []string
	i, err = g.SMSCommand(ctx, "+CMGS=\""+number+"\"", message)
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
func (g *GSM) SendSMSPDU(ctx context.Context, tpdu []byte) (rsp string, err error) {
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
	i, err = g.SMSCommand(ctx, fmt.Sprintf("+CMGS=%d", len(tpdu)), s)
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
