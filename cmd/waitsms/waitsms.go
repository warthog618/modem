// waitsms waits for SMSs to be received by the modem, and dumps them to stdout.
//
// This provides an example of using indications, as well as a test
// that the library works with the modem.
//
// The modem device provided must support nofications, or no SMSs will be seen.
// (the notification port is typically USB2, hence the default)
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/warthog618/sms/ms/message"
	"github.com/warthog618/sms/ms/sar"

	"github.com/warthog618/sms/encoding/tpdu"

	"github.com/warthog618/sms/ms/pdumode"

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
	g.SetPDUMode()
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
	if _, err = g.Command(ctx, "+CNMI=1,2,2,1,0"); err != nil {
		log.Println(err)
		cancel()
		return
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), *period)
	go func() {
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
	}()
	defer cancel()
	pd := pdumode.Decoder{}
	asyncError := func(error) {
		log.Printf("reassembly error: %v", err)
	}
	udd, err := tpdu.NewUDDecoder()
	if err != nil {
		log.Printf("err: %v\n", err)
	}
	c := sar.NewCollector(time.Hour, asyncError)
	reassembler := message.NewReassembler(udd, c)
	defer reassembler.Close()
	for {
		select {
		case <-ctx.Done():
			log.Println("exiting...")
			return
		case i := <-cmt:
			actx, acancel := context.WithTimeout(ctx, *timeout)
			g.Command(actx, "+CNMA")
			acancel()
			lstr := strings.Split(i[0], ",")
			l, err := strconv.Atoi(lstr[len(lstr)-1])
			if err != nil {
				log.Printf("err: %v\n", err)
			}
			_, pdu, err := pd.DecodeString(i[1])
			if err != nil {
				log.Printf("err: %v\n", err)
			}
			if int(l) != len(pdu) {
				log.Printf("length mismatch - expected %d, got %d", l, len(pdu))
			}
			m, err := reassembler.Reassemble(pdu)
			if err != nil {
				log.Printf("err: %v\n", err)
			}
			if m != nil {
				log.Printf("%s: %s\n", m.Number, m.Msg)
			}
		}
	}
}
