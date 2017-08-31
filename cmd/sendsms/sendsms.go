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
	num := flag.String("n", "+12345", "number to send to, in international format")
	msg := flag.String("m", "Zoot Zoot", "the message to send")
	timeout := flag.Duration("t", 100*time.Millisecond, "command timeout period")
	flag.Parse()

	m, err := serial.New(*dev, *baud)
	if err != nil {
		log.Println(err)
		return
	}
	// tracing for development
	l := log.New(os.Stdout, "", log.LstdFlags)
	trt := trace.New(m, l)
	//	trh := trace.New(trt, l, trace.HexMode())
	g := gsm.New(trt)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err = g.Init(ctx); err != nil {
		log.Println(err)
		return
	}
	g.Command(ctx, "+CMGF=1") // switch to TEXT MSG mode
	mr, err := g.SendSMS(ctx, *num, *msg)
	// !!! check CPIN?? on failure to determine root cause??  If ERROR 302
	log.Printf("%v %v\n", mr, err)
}
