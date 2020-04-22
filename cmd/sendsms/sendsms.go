// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// sendsms sends an SMS using the modem.
//
// This provides an example of using the SendSMS command, as well as a test
// that the library works with the modem.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"time"

	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
	"github.com/warthog618/sms"
)

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	num := flag.String("n", "+12345", "number to send to, in international format")
	msg := flag.String("m", "Zoot Zoot", "the message to send")
	timeout := flag.Duration("t", 5000*time.Millisecond, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	pdumode := flag.Bool("p", false, "send in PDU mode")
	hex := flag.Bool("x", false, "hex dump modem responses")
	flag.Parse()

	m, err := serial.New(serial.WithPort(*dev), serial.WithBaud(*baud))
	if err != nil {
		log.Fatal(err)
	}
	var mio io.ReadWriter = m
	if *hex {
		mio = trace.New(m, log.New(os.Stdout, "", log.LstdFlags), trace.ReadFormat("r: %v"))
	} else if *verbose {
		mio = trace.New(m, log.New(os.Stdout, "", log.LstdFlags))
	}
	g := gsm.New(mio)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if *pdumode {
		g.SetPDUMode()
	}
	if err = g.Init(ctx); err != nil {
		log.Fatal(err)
	}
	if *pdumode {
		sendPDU(ctx, g, *num, *msg)
		return
	}
	mr, err := g.SendSMS(ctx, *num, *msg)
	// !!! check CPIN?? on failure to determine root cause??  If ERROR 302
	log.Printf("%v %v\n", mr, err)
}

func sendPDU(ctx context.Context, g *gsm.GSM, number string, msg string) {
	pdus, err := sms.Encode([]byte(msg), sms.To(number), sms.WithAllCharsets)
	if err != nil {
		log.Fatal(err)
	}
	for i, p := range pdus {
		tp, err := p.MarshalBinary()
		if err != nil {
			log.Fatal(err)
		}
		mr, err := g.SendSMSPDU(ctx, tp)
		if err != nil {
			// !!! check CPIN?? on failure to determine root cause??  If ERROR 302
			log.Fatal(err)
		}
		log.Printf("PDU %d: %v\n", i+1, mr)
	}
}
