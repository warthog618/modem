// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// Package serial provides a serial port, which provides the io.ReadWriter interface,
// that provides the connection between the at or gsm packages and the physical modem.
package serial

import (
	"github.com/tarm/serial"
	"time"
)

// New creates a serial port.
//
// This is currently a simple wrapper around tarm serial.
func New(options ...Option) (*serial.Port, error) {
	cfg := defaultConfig
	for _, option := range options {
		option.applyConfig(&cfg)
	}

	config := serial.Config{Name: cfg.port, Baud: cfg.baud, ReadTimeout: cfg.ReadTimeout}
	p, err := serial.OpenPort(&config)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// WithBaud sets the baud rate for the serial port.
func WithBaud(b int) Baud {
	return Baud(b)
}

// WithPort specifies the port for the serial port.
func WithPort(p string) Port {
	return Port(p)
}

// WithTimeout specifies read timeout the serial port.
func WithTimeout(t time.Duration) ReadTimeout {
	return ReadTimeout(t)
}

// Option is a construction option that modifies the behaviour of the serial port.
type Option interface {
	applyConfig(*Config)
}

// Config contains the configuration parameters of the serial port.
type Config struct {
	port string
	baud int
	ReadTimeout time.Duration // Total timeout
}

// Baud is the bit rate for the serial line.
type Baud int

func (b Baud) applyConfig(c *Config) {
	c.baud = int(b)
}

// Port identifies the serial port on the plaform.
type Port string

func (p Port) applyConfig(c *Config) {
	c.port = string(p)
}

type ReadTimeout time.Duration

func (t ReadTimeout) applyConfig(c *Config) {
	c.ReadTimeout = time.Duration(t)
}