modem
=======

A low level Go driver for AT modems.

modem is a Go library for interacting with AT based modems.
The initial impetus is to provide functionality to send and receive SMSs via
a GSM modem, but the library may be generally useful for any device controlled
by AT commands.

The AT package provides a lowl level driver which sits between an io.ReadWriter,
representing the physical modem, and a higher level driver or application.
The AT driver provides the ability to issue AT commands to the modem, and to
receive the info and status returned by the modem, as synchronous function calls.
Handlers for asynchronous indications from the modem, such as received SMSs,
can be registered with the driver.

The GSM package adds a higher level SendSMS method to the AT driver, that allows
for sending SMSs without any knowledge of the underlying AT commands.

The info package provides utility functions to manipulate the info returned in
the responses from the modem.

The serial package provides a simple wrapper around a third party serial driver,
so you don't have to find one yourself.

The trace package provides a driver, which may be inserted between the AT driver
and the underlying modem, to log interactions with the modem for debugging
purposes.

## Features ##

Supports the following functionality:
- Simple synchronous interface for AT commands
- Serialises access to the modem from multiple goroutines
- Context support to allow higher layers to specify timeouts
- Asynchronous indication handing
- Tracing of messages to and from the modem
- Pluggable serial driver - any io.ReadWriter will suffice

## Usage ##

Refer to package documentation, tests and example commands.

## TODO ##

- Encoding/Decoding of SMS PDUs
