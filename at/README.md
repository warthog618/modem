# at

A low level Go driver for AT modems.

[![Build Status](https://travis-ci.org/warthog618/modem.svg)](https://travis-ci.org/warthog618/modem)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/warthog618/modem/at)
[![Coverage Status](https://coveralls.io/repos/github/warthog618/modem/badge.svg?branch=master)](https://coveralls.io/github/warthog618/modem?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/warthog618/modem)](https://goreportcard.com/report/github.com/warthog618/modem)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/warthog618/modem/blob/master/LICENSE)

The **at** package provides a low level driver which sits between an
io.ReadWriter, representing the physical modem, and a higher level driver or
application.

The AT driver provides the ability to issue AT commands to the modem, and to
receive the info and status returned by the modem, as synchronous function
calls.

Handlers for asynchronous indications from the modem, such as received SMSs,
can be registered with the driver.

## Features

Supports the following functionality:

- Simple synchronous interface for AT commands
- Serialises access to the modem from multiple goroutines
- Asynchronous indication handling
- Pluggable serial driver - any io.ReadWriter will suffice

## Usage

### Construction

The modem is constructed with *New*:

```go
modem := at.New(ioWR)
```

Some modem behaviour can be controlled using optional parameters:

```go
modem := at.New(ioWR, at.WithTimeout(time.Second))
```

This example sets the default timeout for AT commands.

### Modem Init

The modem can be initialised to a known state using *Init*:

```go
err := modem.Init()
```

By default the Init issues the **ATZ** and **ATE0** commands.  The set of
commands performed can be replaced using the optional *WithCmds* parameter:

```go
err := modem.Init(at.WithCmds("Z","^CURC=0"))
```

### AT Commands

Issue AT commands to the modem and receive the response using *Command*:

```go
info, err := modem.Command("I")
```

This produces the following interaction with the modem (exact results will differ for your modem):

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

### SMS Commands

SMS commands are a special case as they are a two stage process, with the modem
prompting between stages.  The *SMSCommand* performs the two stage handshake
with the modem and returns any resulting info:

```go
info, err := modem.SMSCommand("+CMGS=\"12345\"", "hello world")
```

This example sends an SMS with the modem in text mode.

### Asynchronous Indications

Handlers can be provided for asynchronous indications using *AddIndication*:

```go
handler := func(info []string) {
    // handle CMT info here
}
err := modem.AddIndication("+CMT:", handler)
```

This example provides a handler for **+CMT** events.

The handler can be removed using *CancelIndication*:

```go
modem.CancelIndication("+CMT:")
```

### Options

A number of the modem methods accept optional parameters.  The following table comprises a list of the available options:

Option | Method | Description
---|---|---
WithTimeout(time.duration)|New, Init, Command, SMSCommand| Specify the timeout for commands.  A value provided to New becomes the default for the other methods.
WithCmds([]string)|New, Init| Override the set of commands issued by Init.
WithEscTime(time.Duration)|New|Specifies the minimum period between issuing an escape and a subsequent command.
WithIndication(prefix, handler)|New| Adds an indication handler at construction time.
WithTrailingLines(int)|AddIndication, WithIndication| Specifies the number of lines to collect following the indicationline itself.
WithTrailingLine|AddIndication, WithIndication| Simple case of one trailing line.
