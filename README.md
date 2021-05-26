# modem

A low level Go driver for AT modems.

[![Build Status](https://travis-ci.org/warthog618/modem.svg)](https://travis-ci.org/warthog618/modem)
[![Coverage Status](https://coveralls.io/repos/github/warthog618/modem/badge.svg?branch=master)](https://coveralls.io/github/warthog618/modem?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/vasjaj/modem)](https://goreportcard.com/report/github.com/vasjaj/modem)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/vasjaj/modem/blob/master/LICENSE)

modem is a Go library for interacting with AT based modems.

The initial impetus was to provide functionality to send and receive SMSs via a
GSM modem, but the library may be generally useful for any device controlled by
AT commands.

The [at](at) package provides a low level driver which sits between an
io.ReadWriter, representing the physical modem, and a higher level driver or
application.

The AT driver provides the ability to issue AT commands to the modem, and to
receive the info and status returned by the modem, as synchronous function
calls.

Handlers for asynchronous indications from the modem, such as received SMSs,
can be registered with the driver.

The [gsm](gsm) package wraps the AT driver to add higher level functions to
send and receive SMS messages, including long messages split into multiple
parts, without any knowledge of the underlying AT commands.

The [info](info) package provides utility functions to manipulate the info
returned in the responses from the modem.

The [serial](serial) package provides a simple wrapper around a third party
serial driver, so you don't have to find one yourself.

The [trace](trace) package provides a driver, which may be inserted between the
AT driver and the underlying modem, to log interactions with the modem for
debugging purposes.

The [cmd](cmd) directory contains basic commands to exercise the library and a
modem, including [retrieving details](cmd/modeminfo/modeminfo.go) from the
modem, [sending](cmd/sendsms/sendsms.go) and
[receiving](cmd/waitsms/waitsms.go) SMSs, and
[retrieving](cmd/phonebook/phonebook.go) the SIM phonebook.

## Features

Supports the following functionality:

- Simple synchronous interface for AT commands
- Serialises access to the modem from multiple goroutines
- Asynchronous indication handling
- Tracing of messages to and from the modem
- Pluggable serial driver - any io.ReadWriter will suffice

## Usage

The [at](at) package allows you to issue commands to the modem and receive the
response. e.g.:

```go
modem := at.New(ioWR)
info, err := modem.Command("I")
```

produces the following interaction with the modem (exact results will differ for your modem):

```shell
2018/05/17 20:39:56 w: ATI
2018/05/17 20:39:56 r:
Manufacturer: huawei
Model: E173
Revision: 21.017.09.00.314
IMEI: 1234567
+GCAP: +CGSM,+DS,+ES

OK
```

and returns this info:

```go
info = []string{
    "Manufacturer: huawei",
    "Model: E173",
    "Revision: 21.017.09.00.314",
    "IMEI: 1234567",
    "+GCAP: +CGSM,+DS,+ES",
    }
```

Refer to the [modeminfo](cmd/modeminfo/modeminfo.go) for an example of how to create a modem object such as the one used in this example.

For more information, refer to package documentation, tests and example commands.

Package | Documentation | Tests | Example code
------- | ------------- | ----- | ------------
[at](at) | [![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/at) | [at_test](at/at_test.go) | [modeminfo](cmd/modeminfo/modeminfo.go)
[gsm](gsm) | [![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/gsm) | [gsm_test](gsm/gsm_test.go) | [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
[info](info) | [![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/info) | [info_test](info/info_test.go) | [phonebook](cmd/phonebook/phonebook.go)
[serial](serial) | [![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/serial) | [serial_test](serial/serial_test.go) | [modeminfo](cmd/modeminfo/modeminfo.go), [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
[trace](trace) | [![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/trace) | [trace_test](trace/trace_test.go) | [sendsms](cmd/sendsms/sendsms.go), [waitsms](cmd/waitsms/waitsms.go)
