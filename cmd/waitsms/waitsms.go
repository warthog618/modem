// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// waitsms waits for SMSs to be received by the modem, and dumps them to
// stdout.
//
// This provides an example of using indications, as well as a test that the
// library works with the modem.
//
// The modem device provided must support nofications, or no SMSs will be seen.
// (the notification port is typically USB2, hence the default)
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/tpdu"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
)

func main() {
	dev := flag.String("d", "/dev/ttyUSB2", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	period := flag.Duration("p", 10*time.Minute, "period to wait")
	timeout := flag.Duration("t", 400*time.Millisecond, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	hex := flag.Bool("x", false, "hex dump modem responses")
	flag.Parse()
	m, err := serial.New(serial.WithPort(*dev), serial.WithBaud(*baud))
	if err != nil {
		log.Println(err)
		return
	}
	defer m.Close()
	var mio io.ReadWriter = m
	if *hex {
		mio = trace.New(m, trace.WithReadFormat("r: %v"))
	} else if *verbose {
		mio = trace.New(m)
	}
	g := gsm.New(at.New(mio, at.WithTimeout(*timeout)), gsm.WithPDUMode)
	err = g.Init()
	if err != nil {
		log.Println(err)
		return
	}
	go pollSignalQuality(g, timeout)
	waitForSMSs(g)
	for {
		select {
		case <-time.After(*period):
			log.Println("exiting...")
			return
		case <-g.Closed():
			log.Fatal("modem closed, exiting...")
		}
	}
}

// pollSignalQuality polls the modem to read signal quality every minute.
//
// This is run in parallel to waitForSMSs to demonstrate separate goroutines
// interacting with the modem.
func pollSignalQuality(g *gsm.GSM, timeout *time.Duration) {
	for {
		select {
		case <-time.After(time.Minute):
			i, err := g.Command("+CSQ")
			if err != nil {
				log.Println(err)
			} else {
				log.Printf("Signal quality: %v\n", i)
			}
		case <-g.Closed():
			return
		}
	}
}

func unmarshalTPDU(info []string) (tp tpdu.TPDU, err error) {
	if info == nil {
		err = errors.New("received nil info")
		return
	}
	lstr := strings.Split(info[0], ",")
	l, serr := strconv.Atoi(lstr[len(lstr)-1])
	if serr != nil {
		err = serr
		return
	}
	pdu, perr := pdumode.UnmarshalHexString(info[1])
	if perr != nil {
		err = perr
		return
	}
	if int(l) != len(pdu.TPDU) {
		err = fmt.Errorf("length mismatch - expected %d, got %d", l, len(pdu.TPDU))
		return
	}
	err = tp.UnmarshalBinary(pdu.TPDU)
	return
}

// waitForSMSs adds an indication to the modem and prints any received SMSs.
//
// It will continue to wait until the provided context is done.
// It reassembles multi-part SMSs into a complete message prior to display.
func waitForSMSs(g *gsm.GSM) error {
	c := sms.NewCollector()
	cmtHandler := func(info []string) {
		g.Command("+CNMA")
		tp, err := unmarshalTPDU(info)
		if err != nil {
			log.Printf("err: %v\n", err)
			return
		}
		tpdus, err := c.Collect(tp)
		if err != nil {
			log.Printf("err: %v\n", err)
			return
		}
		m, err := sms.Decode(tpdus)
		if err != nil {
			log.Printf("err: %v\n", err)
		}
		if m != nil {
			log.Printf("%s: %s\n", tpdus[0].OA.Number(), m)
		}
	}
	err := g.AddIndication("+CMT:", cmtHandler, at.WithTrailingLine)
	if err != nil {
		return err
	}
	// tell the modem to forward SMSs to us.
	_, err = g.Command("+CNMI=1,2,2,1,0")
	return err
}
