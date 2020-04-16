// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

package serial_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warthog618/modem/serial"
)

func TestNew(t *testing.T) {
	if _, err := os.Stat("/dev/ttyUSB0"); os.IsNotExist(err) {
		t.Skip("no modem available")
	}
	m, err := serial.New("/dev/ttyUSB0", 115200)
	require.NotNil(t, err)
	require.NotNil(t, m)
	m.Close()
}

func TestNewFail(t *testing.T) {
	// bogus path
	m, err := serial.New("bogusmodem", 115200)
	assert.IsType(t, &os.PathError{}, err)
	assert.Nil(t, m)
}
