package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/warthog618/modem/gsm"
	"github.com/warthog618/modem/info"
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
	//	tr := trace.New(m, log.New(os.Stdout, "", log.LstdFlags))
	g := gsm.New(m)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	g.Init(ctx)
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
