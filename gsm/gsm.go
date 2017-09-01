package gsm

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/info"
)

// GSM modem decorates the AT modem with GSM specific functionality.
type GSM struct {
	*at.AT
	mode mode
	sca  string
}

type mode int

const (
	modeText mode = iota
	modePDU
)

// New creates a new GSM modem.
func New(modem io.ReadWriter) *GSM {
	return &GSM{at.New(modem), modeText, ""}
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
		"+CMEE=2", // textual errors
		"+CMGF=1", // text mode for now, later check if supports PDU and switch?
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
// The mr is returned on success, else an error.
// In text mode the message must be a message string.
// In PDU mode the message may be a message string OR a hex coded SMS PDU. (not currently supported)
func (g *GSM) SendSMS(ctx context.Context, number string, message string) (string, error) {
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

var (
	// ErrNotGSMCapable indicates that the modem does not support the GSM
	// command set, as determined from the GCAP response.
	ErrNotGSMCapable = errors.New("modem is not GSM capable")
	// ErrNotPINReady indicates the modem SIM card is not ready to perform operations.
	ErrNotPINReady = errors.New("modem is not PIN Ready")
	// ErrMalformedResponse indicates the modem returned .
	ErrMalformedResponse = errors.New("modem returned malformed response")
)
