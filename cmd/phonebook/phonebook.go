// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// phonebook dumps the contents of the modem SIM phonebook.
//
// This provides an example of processing the info returned by the modem.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/info"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
)

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	timeout := flag.Duration("t", 400*time.Millisecond, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	flag.Parse()
	m, err := serial.New(serial.WithPort(*dev), serial.WithBaud(*baud))
	if err != nil {
		log.Println(err)
		return
	}
	var mio io.ReadWriter = m
	if *verbose {
		mio = trace.New(m)
	}
	g := gsm.New(gsm.FromReadWriter(mio))
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	err = g.Init(ctx)
	cancel()
	if err != nil {
		log.Println(err)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), *timeout)
	i, err := g.Command(ctx, "+CPBR=1,99")
	cancel()
	if err != nil {
		log.Println(err)
		return
	}
	for _, l := range i {
		if !info.HasPrefix(l, "+CPBR") {
			continue
		}
		entry := strings.Split(info.TrimPrefix(l, "+CPBR"), ",")
		nameh := []byte(strings.Trim(entry[3], "\""))
		name := make([]byte, hex.DecodedLen(len(nameh)))
		n, err := hex.Decode(name, nameh)
		if err != nil {
			log.Fatal("decode error ", err)
		}
		fmt.Printf("%2s %-10s %s\n", entry[0], strings.Trim(entry[1], "\""), name[:n])
	}
}
