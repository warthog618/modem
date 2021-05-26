# gsm

A high level Go driver for GSM modems.

[![Build Status](https://travis-ci.org/warthog618/modem.svg)](https://travis-ci.org/warthog618/modem)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/vasjaj/modem/gsm)
[![Coverage Status](https://coveralls.io/repos/github/warthog618/modem/badge.svg?branch=master)](https://coveralls.io/github/warthog618/modem?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/vasjaj/modem)](https://goreportcard.com/report/github.com/vasjaj/modem)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/vasjaj/modem/blob/master/LICENSE)

The **gsm** package provides a wrapper around [**at**](../at) that supports sending and receiving SMS messages, including long multi-part messages.

## Features

Supports the following functionality:

- Simple synchronous interface for sending messages
- Both text and PDU mode interface to GSM modem
- Asynchronous handling of received messages

## Usage

### Construction

The GSM modem is constructed with *New*:

```go
modem := gsm.New(atmodem)
```

Some modem behaviour can be controlled using optional parameters. This example
sets the puts the modem into PDU mode:

```go
modem := gsm.New(atmodem, gsm.WithPDUMode)
```

### Modem Init

The modem is reset into a known state and checked that is supports GSM functionality using the *Init* method:

```go
err := modem.Init()
```

The *Init* method is a wrapper around the [**at**](../at) *Init* method, and accepts the same options:

```go
err := modem.Init(at.WithCmds("Z","^CURC=0"))
```

### Sending Short Messages

Send a simple short message that will fit within a single SMS TPDU using
*SendShortMessage*:

```go
mr, err := modem.SendShortMessage("+12345","hello")
```

The modem may be in either text or PDU mode.

### Sending Long Messages

This example sends an SMS with the modem in text mode:

```go
mrs, err := modem.SendLongMessage("+12345", apotentiallylongmessage)
```

### Sending PDUs

Arbitrary SMS TPDUs can be sent using the *SendPDU* method:

```go
mr, err := modem.SendPDU(tpdu)
```

### Receiving Messages

A handler can be provided for received SMS messages using *StartMessageRx*:

```go
handler := func(msg gsm.Message) {
    // handle message here
}
err := modem.StartMessageRx(handler)
```

The handler can be removed using *StopMessageRx*:

```go
modem.StopMessageRx()
```

### Options

A number of the modem methods accept optional parameters.  The following table comprises a list of the available options:

Option | Method | Description
---|---|---
*WithCollector(Collector)*|StartMessageRx| Provide a custom collector to reassemble multi-part SMSs.
*WithEncoderOption(sms.EncoderOption)*|New| Specify options for encoding outgoing messages.
*WithPDUMode*|New|Configure the modem into PDU mode (default).
*WithReassemblyTimeout(time.Duration)*|StartMessageRx| Overrides the time allowed to wait for all the parts of a multi-part message to be received and reassembled.  The default is 24 hours.  This option is ignored if *WithCollector* is also applied.
*WithSCA(pdumode.SMSCAddress)*|New| Override the SCA when sending messages.
*WithTextMode*|New|Configure the modem into text mode.  This is only required to send short messages in text mode, and conflicts with sending long messages or PDUs, as well as receiving messages.
