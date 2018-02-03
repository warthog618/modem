// waitsms waits for SMSs to be received by the modem, and dumps them to stdout.
//
// The output is currently in undecoded PDU format.
//
// This provides an example of using indications, as well as a test
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
)

func main() {
	dev := flag.String("d", "/dev/ttyUSB0", "path to modem device")
	baud := flag.Int("b", 115200, "baud rate")
	period := flag.Duration("p", 10*time.Minute, "period to wait")
	timeout := flag.Duration("t", 400*time.Millisecond, "command timeout period")
	verbose := flag.Bool("v", false, "log modem interactions")
	hex := flag.Bool("x", false, "hex dump modem responses")
	flag.Parse()
	m, err := serial.New(*dev, *baud)
	if err != nil {
		log.Println(err)
		return
	}
	defer m.Close()
	var mio io.ReadWriter = m
	if *hex {
		mio = trace.New(m, log.New(os.Stdout, "", log.LstdFlags), trace.ReadFormat("r: %v"))
	} else if *verbose {
		mio = trace.New(m, log.New(os.Stdout, "", log.LstdFlags))
	}
	g := gsm.New(mio)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	err = g.Init(ctx)
	cancel()
	if err != nil {
		log.Println(err)
		return
	}
	cmt, err := g.AddIndication("+CMT:", 1)
	if err != nil {
		log.Println(err)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), *timeout)
	g.Command(ctx, "+CMGF=0") // switch to PDU MSG mode
	if _, err = g.Command(ctx, "+CNMI=1,2,2,1,0"); err != nil {
		log.Println(err)
		cancel()
		return
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), *period)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			log.Println("exiting...")
			return
		case i := <-cmt:
			// at this point just dump the received SMS.
			// TODO: do decoding and duplicate detection.
			log.Println(i)
			actx, acancel := context.WithTimeout(ctx, *timeout)
			g.Command(actx, "+CNMA")
			acancel()
		}
	}
}
