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
	"context"
	"flag"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/tpdu"

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
	g := gsm.New(gsm.FromReadWriter(mio), gsm.WithPDUMode)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	err = g.Init(ctx)
	cancel()
	if err != nil {
		log.Println(err)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), *period)
	defer cancel()
	go pollSignalQuality(ctx, g, timeout)
	waitForSMSs(ctx, g, timeout)
}

// pollSignalQuality polls the modem to read signal quality every minute.
//
// This is run in parallel to waitForSMSs to demonstrate separate goroutines
// interacting with the modem.
func pollSignalQuality(ctx context.Context, g *gsm.GSM, timeout *time.Duration) {
	for {
		select {
		case <-time.After(time.Minute):
			tctx, tcancel := context.WithTimeout(ctx, *timeout)
			i, err := g.Command(tctx, "+CSQ")
			if err != nil {
				log.Println(err)
			} else {
				log.Printf("Signal quality: %v\n", i)
			}
			tcancel()
		case <-ctx.Done():
			return
		}
	}
}

// waitForSMSs adds an indication to the modem and prints any received SMSs.
//
// It will continue to wait until the provided context is done.
// It reassembles multi-part SMSs into a complete message prior to display.
func waitForSMSs(ctx context.Context, g *gsm.GSM, timeout *time.Duration) {
	cmt, err := g.AddIndication("+CMT:", 1)
	if err != nil {
		log.Println(err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, *timeout)
	// tell the modem to forward SMSs to us.
	if _, err = g.Command(cctx, "+CNMI=1,2,2,1,0"); err != nil {
		log.Println(err)
		cancel()
		return
	}
	cancel()
	reassemblyTimeout := func(tpdus []*tpdu.TPDU) {
		log.Printf("reassembly timeout: %v", tpdus)
	}
	c := sms.NewCollector(sms.WithReassemblyTimeout(time.Hour, reassemblyTimeout))
	defer c.Close()
	for {
		select {
		case <-ctx.Done():
			log.Println("exiting...")
			return
		case i, ok := <-cmt:
			if !ok {
				log.Fatal("modem closed, exiting...")
			}
			if i == nil {
				log.Println("received nil info")
				continue
			}
			actx, acancel := context.WithTimeout(ctx, *timeout)
			g.Command(actx, "+CNMA")
			acancel()
			lstr := strings.Split(i[0], ",")
			l, err := strconv.Atoi(lstr[len(lstr)-1])
			if err != nil {
				log.Printf("err: %v\n", err)
				continue
			}
			pdu, err := pdumode.UnmarshalHexString(i[1])
			if err != nil {
				log.Printf("err: %v\n", err)
				continue
			}
			if int(l) != len(pdu.TPDU) {
				log.Printf("length mismatch - expected %d, got %d", l, len(pdu.TPDU))
				continue
			}
			tp := tpdu.TPDU{}
			err = tp.UnmarshalBinary(pdu.TPDU)
			if err != nil {
				log.Printf("err: %v\n", err)
				continue
			}
			tpdus, err := c.Collect(tp)
			if err != nil {
				log.Printf("err: %v\n", err)
				continue
			}
			m, err := sms.Decode(tpdus)
			if err != nil {
				log.Printf("err: %v\n", err)
			}
			if m != nil {
				log.Printf("%s: %s\n", tpdus[0].OA.Number(), m)
			}
		}
	}
}
