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

// New creates a new GSM modem.
func New(modem io.ReadWriter) *GSM {
	return &GSM{AT: at.New(modem)}
}

// SetSCA sets the SCA used when transmitting SMSs.
//
// This overrides the default set in the SIM.
func (g *GSM) SetSCA(sca pdumode.SMSCAddress) {
	g.sca = sca
}

// SetPDUMode sets the GSM to use PDU mode when transmitting SMSs.
//
// This must be called before Init.
func (g *GSM) SetPDUMode() {
	g.pduMode = true
}

// Init initialises the GSM modem.
func (g *GSM) Init(ctx context.Context) error {
	if err := g.AT.Init(ctx); err != nil {
		return err
	}
	// test GCAP response to ensure +GSM support, and modem sync.
	i, err := g.Command(ctx, "+GCAP")
	if err != nil {
		return err
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
		_, err := g.Command(ctx, cmd)
		if err != nil {
			return err
		}
	}
	return nil
}

// SendSMS sends an SMS message to the number.
//
// The mr is returned on success, else an error.
func (g *GSM) SendSMS(ctx context.Context, number string, message string) (string, error) {
	if g.pduMode {
		return "", ErrWrongMode
	}
	i, err := g.SMSCommand(ctx, "+CMGS=\""+number+"\"", message)
	if err != nil {
		return "", err
	}
	// parse response, ignoring any lines other than well-formed.
	for _, l := range i {
		if info.HasPrefix(l, "+CMGS") {
			return info.TrimPrefix(l, "+CMGS"), nil
		}
	}
	return "", ErrMalformedResponse
}

// SendSMSPDU sends an SMS PDU.
//
// tpdu is the binary TPDU to be sent.
// The mr is returned on success, else an error.
func (g *GSM) SendSMSPDU(ctx context.Context, tpdu []byte) (string, error) {
	if !g.pduMode {
		return "", ErrWrongMode
	}
	pdu := pdumode.PDU{SMSC: g.sca, TPDU: tpdu}
	s, err := pdu.MarshalHexString()
	if err != nil {
		return "", err
	}
	i, err := g.SMSCommand(ctx, fmt.Sprintf("+CMGS=%d", len(tpdu)), s)
	if err != nil {
		return "", err
	}
	// parse response, ignoring any lines other than well-formed.
	for _, l := range i {
		if info.HasPrefix(l, "+CMGS") {
			return info.TrimPrefix(l, "+CMGS"), nil
		}
	}
	return "", ErrMalformedResponse
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
