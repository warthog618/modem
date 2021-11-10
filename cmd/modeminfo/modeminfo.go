// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

// modeminfo collects and displays information related to the modem and its
// current configuration.
//
// This serves as an example of how interact with a modem, as well as
// providing information which may be useful for debugging.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
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
	defer m.Close()
	var mio io.ReadWriter = m
	if *verbose {
		mio = trace.New(m)
	}
	a := at.New(mio, at.WithTimeout(*timeout))
	err = a.Init()
	if err != nil {
		log.Println(err)
		return
	}
	cmds := []string{
		"I",
		"+GCAP",
		"+CMEE=2",
		"+CGMI",
		"+CGMM",
		"+CGMR",
		"+CGSN",
		"+CSQ",
		"+CIMI",
		"+CREG?",
		"+CNUM",
		"+CPIN?",
		"+CEER",
		"+CSCA?",
		"+CSMS?",
		"+CSMS=?",
		"+CPMS=?",
		"+CCID?",
		"+CCID=?",
		"^ICCID?",
		"+CNMI?",
		"+CNMI=?",
		"+CNMA=?",
		"+CMGF?",
		"+CMGF=?",
		"+CUSD?",
		"+CUSD=?",
		"^USSDMODE?",
		"^USSDMODE=?",
	}
	for _, cmd := range cmds {
		info, err := a.Command(cmd)
		fmt.Println("AT" + cmd)
		if err != nil {
			fmt.Printf(" %s\n", err)
			continue
		}
		for _, l := range info {
			fmt.Printf(" %s\n", l)
		}
	}
}
