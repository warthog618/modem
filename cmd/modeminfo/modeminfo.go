// modeminfo collects and displays information relatied to the modem and its
// current configuration.
//
// This serves as an example of how interact with a modem, as well as
// providing information which may be useful for debugging.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/warthog618/modem/at"
	"github.com/warthog618/modem/serial"
)

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	timeout := flag.Duration("t", 100*time.Millisecond, "command timeout period")
	flag.Parse()
	m, err := serial.New(*dev, *baud)
	if err != nil {
		log.Println(err)
		return
	}
	defer m.Close()
	a := at.New(m)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	err = a.Init(ctx)
	cancel()
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
		"+CNMI?",
		"+CNMI=?",
		"+CNMA=?",
		"+CMGF=?",
	}
	for _, cmd := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		info, err := a.Command(ctx, cmd)
		cancel()
		fmt.Println("AT" + cmd)
		if err != nil {
			fmt.Printf(" %s\n", err)
		} else {
			for _, l := range info {
				fmt.Printf(" %s\n", l)
			}
		}
	}
}
