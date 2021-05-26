// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// phonebook dumps the contents of the modem SIM phonebook.
//
// This provides an example of processing the info returned by the modem.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/vasjaj/modem/at"
	"github.com/vasjaj/modem/gsm"
	"github.com/vasjaj/modem/info"
	"github.com/vasjaj/modem/serial"
	"github.com/vasjaj/modem/trace"
)

var version = "undefined"

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	timeout := flag.Duration("t", 400*time.Millisecond, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	vsn := flag.Bool("version", false, "report version and exit")
	flag.Parse()
	if *vsn {
		fmt.Printf("%s %s\n", os.Args[0], version)
		os.Exit(0)
	}
	m, err := serial.New(serial.WithPort(*dev), serial.WithBaud(*baud))
	if err != nil {
		log.Println(err)
		return
	}
	var mio io.ReadWriter = m
	if *verbose {
		mio = trace.New(m)
	}
	g := gsm.New(at.New(mio, at.WithTimeout(*timeout)))
	err = g.Init()
	if err != nil {
		log.Println(err)
		return
	}
	i, err := g.Command("+CPBR=1,99")
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
