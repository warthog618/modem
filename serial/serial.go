package serial

import (
	"github.com/tarm/serial"
)

// New is currently a simple wrapper around tarm serial
func New(comPort string, baudRate int) (*serial.Port, error) {
	config := &serial.Config{Name: comPort, Baud: baudRate}
	p, err := serial.OpenPort(config)
	if err != nil {
		return nil, err
	}
	return p, nil
}
