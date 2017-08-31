package main

import (
	"context"
	"flag"
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
	timeout := flag.Duration("t", 200*time.Millisecond, "command timeout period")
	flag.Parse()
	m, err := serial.New(*dev, *baud)
	if err != nil {
		log.Println(err)
		return
	}
	defer m.Close()
	// tracing for debugging
	l := log.New(os.Stdout, "", log.LstdFlags)
	tr := trace.New(m, l)
	//	tr := trace.New(trt, l, trace.ReadFormat("r: %v")
	g := gsm.New(tr)
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
