// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// sendsms sends an SMS using the modem.
//
// This provides an example of using the SendSMS command, as well as a test
// that the library works with the modem.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
	"github.com/warthog618/sms"
)

var version = "undefined"

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	num := flag.String("n", "+12345", "number to send to, in international format")
	msg := flag.String("m", "Zoot Zoot", "the message to send")
	timeout := flag.Duration("t", 5*time.Second, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	pdumode := flag.Bool("p", false, "send in PDU mode")
	hex := flag.Bool("x", false, "hex dump modem responses")
	vsn := flag.Bool("version", false, "report version and exit")
	flag.Parse()
	if *vsn {
		fmt.Printf("%s %s\n", os.Args[0], version)
		os.Exit(0)
	}
	m, err := serial.New(serial.WithPort(*dev), serial.WithBaud(*baud))
	if err != nil {
		log.Fatal(err)
	}
	var mio io.ReadWriter = m
	if *hex {
		mio = trace.New(m, trace.WithReadFormat("r: %v"))
	} else if *verbose {
		mio = trace.New(m)
	}
	gopts := []gsm.Option{}
	if !*pdumode {
		gopts = append(gopts, gsm.WithTextMode)
	}
	g := gsm.New(at.New(mio, at.WithTimeout(*timeout)), gopts...)
	if err = g.Init(); err != nil {
		log.Fatal(err)
	}
	if *pdumode {
		sendPDU(g, *num, *msg)
		return
	}
	mr, err := g.SendShortMessage(*num, *msg)
	// !!! check CPIN?? on failure to determine root cause??  If ERROR 302
	log.Printf("%v %v\n", mr, err)
}

func sendPDU(g *gsm.GSM, number string, msg string) {
	pdus, err := sms.Encode([]byte(msg), sms.To(number), sms.WithAllCharsets)
	if err != nil {
		log.Fatal(err)
	}
	for i, p := range pdus {
		tp, err := p.MarshalBinary()
		if err != nil {
			log.Fatal(err)
		}
		mr, err := g.SendPDU(tp)
		if err != nil {
			// !!! check CPIN?? on failure to determine root cause??  If ERROR 302
			log.Fatal(err)
		}
		log.Printf("PDU %d: %v\n", i+1, mr)
	}
}
