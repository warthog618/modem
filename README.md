modem
=======

A low level Go driver for AT modems.

[![Build Status](https://travis-ci.org/warthog618/modem.svg)](https://travis-ci.org/warthog618/modem)
[![Go Report Card](https://goreportcard.com/badge/github.com/warthog618/modem)](https://goreportcard.com/report/github.com/warthog618/modem)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/warthog618/modem/blob/master/LICENSE)

modem is a Go library for interacting with AT based modems.
The initial impetus was to provide functionality to send and receive SMSs via
a GSM modem, but the library may be generally useful for any device controlled
by AT commands.

The [AT](at) package provides a low level driver which sits between an io.ReadWriter,
representing the physical modem, and a higher level driver or application.
The AT driver provides the ability to issue AT commands to the modem, and to
receive the info and status returned by the modem, as synchronous function calls.
Handlers for asynchronous indications from the modem, such as received SMSs,
can be registered with the driver.

The [GSM](gsm) package adds higher level SendSMS and SendSMSPDU methods to the AT driver, that allows
for sending SMSs without any knowledge of the underlying AT commands.

The [info](info) package provides utility functions to manipulate the info returned in
the responses from the modem.

The [serial](serial) package provides a simple wrapper around a third party serial driver,
so you don't have to find one yourself.

The [trace](trace) package provides a driver, which may be inserted between the AT driver
and the underlying modem, to log interactions with the modem for debugging
purposes.

The [cmd](cmd) directory contains basic commands to exercise the library and a modem, including
[retrieving details](cmd/modeminfo/modeminfo.go) from the modem, [sending](cmd/sendsms/sendsms.go)
and [receiving](cmd/waitsms/waitsms.go) SMSs, and [retrieving](cmd/phonebook/phonebook.go) the SIM phonebook.

## Features ##

Supports the following functionality:
- Simple synchronous interface for AT commands
- Serialises access to the modem from multiple goroutines
- Context support to allow higher layers to specify timeouts
- Asynchronous indication handling
- Tracing of messages to and from the modem
- Pluggable serial driver - any io.ReadWriter will suffice

## Usage ##

Refer to package documentation, tests and example commands.

Package | Documentation | Tests | Example code
------- | ------------- | ----- | ------------
[at](at) | [![GoDoc](https://godoc.org/github.com/warthog618/modem/at?status.svg)](https://godoc.org/github.com/warthog618/modem/at) | [at_test](at/at_test.go) | [modeminfo](cmd/modeminfo/modeminfo.go)
[gsm](gsm) | [![GoDoc](https://godoc.org/github.com/warthog618/modem/gsm?status.svg)](https://godoc.org/github.com/warthog618/modem/gsm) | [gsm_test](gsm/gsm_test.go) | [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
[info](info) | [![GoDoc](https://godoc.org/github.com/warthog618/modem/info?status.svg)](https://godoc.org/github.com/warthog618/modem/info) | [info_test](info/info_test.go) | [phonebook](cmd/phonebook/phonebook.go)
[serial](serial) | [![GoDoc](https://godoc.org/github.com/warthog618/modem/serial?status.svg)](https://godoc.org/github.com/warthog618/modem/serial) | [serial_test](serial/serial_test.go) | [modeminfo](cmd/modeminfo/modeminfo.go), [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
[trace](trace) | [![GoDoc](https://godoc.org/github.com/warthog618/modem/trace?status.svg)](https://godoc.org/github.com/warthog618/modem/trace) | [trace_test](trace/trace_test.go) | [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
