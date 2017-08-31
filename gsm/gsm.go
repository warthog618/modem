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

func (g *GSM) Init(ctx context.Context) error {
	if err := g.AT.Init(ctx); err != nil {
		return err
	}
	// test GCAP response to ensure +GSM support, and modem sync.
	i, err := g.Command(ctx, "+GCAP")
	if err != nil {
		return err
	}
	l := strings.Split(info.TrimPrefix(i[0], "+GCAP"), ",")
	capabilities := make(map[string]bool)
	for _, cap := range l {
		capabilities[cap] = true
	}
	if !capabilities["+CGSM"] {
		return ErrNotGSMCapable
	}
	cmds := []string{
		"+CMEE=2",
		"+CMGF=1", // text mode for now, later check if supports PDU and switch
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
// In PDU mode the message may be a message string OR a hex coded SMS PDU.
func (g *GSM) SendSMS(ctx context.Context, number string, message string) (string, error) {
	i, err := g.SMSCommand(ctx, "+CMGS=\""+number+"\"", message)
	if err != nil {
		return "", err
	}
	return info.TrimPrefix(i[0], "+CMGS"), nil
}

var (
	// ErrNotGSMCapable indicates that the modem does not support the GSM
	// command set, as determined from the GCAP response.
	ErrNotGSMCapable = errors.New("gsm: modem is not GSM capable")
	// ErrNotPINReady indicates the modem SIM card is not ready to perform operations.
	ErrNotPINReady = errors.New("gsm: modem is not PIN Ready")
)
