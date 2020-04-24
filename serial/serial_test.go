// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

package serial_test

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/warthog618/modem/serial"
)

func modemExists(name string) func(t *testing.T) {
	return func(t *testing.T) {
		if _, err := os.Stat(name); os.IsNotExist(err) {
			t.Skip("no modem available")
		}
	}
}
func TestNew(t *testing.T) {
	patterns := []struct {
		name    string
		prereq  func(t *testing.T)
		options []serial.Option
		err     error
	}{
		{
			"default",
			modemExists("/dev/ttyUSB0"),
			nil,
			nil,
		},
		{
			"empty",
			modemExists("/dev/ttyUSB0"),
			[]serial.Option{},
			nil,
		},
		{
			"baud",
			modemExists("/dev/ttyUSB0"),
			[]serial.Option{serial.WithBaud(9600)},
			nil,
		},
		{
			"port",
			modemExists("/dev/ttyUSB0"),
			[]serial.Option{serial.WithPort("/dev/ttyUSB0")},
			nil,
		},
		{
			"bad port",
			nil,
			[]serial.Option{serial.WithPort("nosuchmodem")},
			&os.PathError{Op: "open", Path: "nosuchmodem", Err: syscall.Errno(2)},
		},
		{
			"bad baud",
			modemExists("/dev/ttyUSB0"),
			[]serial.Option{serial.WithBaud(1234)},
			errors.New("Unrecognized baud rate"),
		},
	}
	for _, p := range patterns {
		f := func(t *testing.T) {
			if p.prereq != nil {
				p.prereq(t)
			}
			m, err := serial.New(p.options...)
			require.Equal(t, p.err, err)
			require.Equal(t, err == nil, m != nil)
			if m != nil {
				m.Close()
			}
		}
		t.Run(p.name, f)
	}
}
