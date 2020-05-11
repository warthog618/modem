// SPDX-License-Identifier: MIT
//
// Copyright © 2020 Kent Gibson <warthog618@gmail.com>.

// ussd sends an USSD message using the modem.
//
// This provides an example of using commands and indications.
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

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/info"
	"github.com/warthog618/modem/serial"
	"github.com/warthog618/modem/trace"
	"github.com/warthog618/sms/encoding/gsm7"
)

var version = "undefined"

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	dcs := flag.Int("n", 15, "DCS field")
	msg := flag.String("m", "*101#", "the message to send")
	timeout := flag.Duration("t", 5*time.Second, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
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
	if *verbose {
		mio = trace.New(m)
	}
	a := at.New(mio, at.WithTimeout(*timeout))
	if err = a.Init(); err != nil {
		log.Fatal(err)
	}
	rspChan := make(chan string)
	handler := func(info []string) {
		rspChan <- info[0]
	}
	a.AddIndication("+CUSD:", handler)
	hmsg := strings.ToUpper(hex.EncodeToString(gsm7.Pack7BitUSSD([]byte(*msg), 0)))
	cmd := fmt.Sprintf("+CUSD=1,\"%s\",%d", hmsg, *dcs)
	_, err = a.Command(cmd)
	if err != nil {
		log.Fatal(err)
	}
	select {
	case <-time.After(*timeout):
		fmt.Println("No response...")
	case rsp := <-rspChan:
		fields := strings.Split(info.TrimPrefix(rsp, "+CUSD"), ",")
		rspb, _ := hex.DecodeString(strings.Trim(fields[1], "\""))
		rspb = gsm7.Unpack7BitUSSD(rspb, 0)
		fmt.Println(string(rspb))
	}
}
