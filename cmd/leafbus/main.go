package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/brutella/can"

	"github.com/slim-bean/leafbus/pkg/charge"
	"github.com/slim-bean/leafbus/pkg/hydra"
	"github.com/slim-bean/leafbus/pkg/push"
	"github.com/slim-bean/leafbus/pkg/stream"
)

func main() {
	address := flag.String("cortex-address", "localhost:9002", "GRPC address and port to find cortex")

	log.Println("Finding interface")
	iface, err := net.InterfaceByName("can0")

	if err != nil {
		log.Fatalf("Could not find network interface %s (%v)", "can0", err)
	}
	log.Println("Opening interface")
	conn, err := can.NewReadWriteCloserForInterface(iface)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating new Charge Monitor")
	m, err := charge.NewMonitor("http://172.20.31.75")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating streamer")
	strm := stream.NewStreamer()

	handler, err := push.NewHandler(*address, strm)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating Hydra monitor")
	hyd, err := hydra.NewHydra(handler, "/dev/ttyUSB0")
	if err != nil {
		log.Println(err)
	} else {
		err = hyd.EnterBinaryMode()
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Creating new Bus and subscribing")
	bus := can.NewBus(conn)
	bus.SubscribeFunc(m.Handle)
	bus.SubscribeFunc(handler.Handle)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)

	go func() {
		select {
		case <-c:
			bus.Disconnect()
			os.Exit(1)
		}
	}()

	log.Println("Starting web server")
	http.HandleFunc("/stream", strm.Handler)
	go func() {
		if err := http.ListenAndServe(":7777", nil); err != nil {
			log.Println(err)
		}
	}()

	log.Println("Entering publish loop")
	err = bus.ConnectAndPublish()
	if err != nil {
		log.Println(err)
	}
	log.Println("Exiting")
}

// logCANFrame logs a frame with the same format as candump from can-utils.
func logCANFrame(frm can.Frame) {
	data := trimSuffix(frm.Data[:], 0x00)
	length := fmt.Sprintf("[%x]", frm.Length)
	log.Printf("%-3s %-4x %-3s % -24X '%s'\n", "can0", frm.ID, length, data, printableString(data[:]))
}

// trim returns a subslice of s by slicing off all trailing b bytes.
func trimSuffix(s []byte, b byte) []byte {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != b {
			return s[:i+1]
		}
	}

	return []byte{}
}

// printableString creates a string from s and replaces non-printable bytes (i.e. 0-32, 127)
// with '.' – similar how candump from can-utils does it.
func printableString(s []byte) string {
	var ascii []byte
	for _, b := range s {
		if b < 32 || b > 126 {
			b = byte('.')

		}
		ascii = append(ascii, b)
	}

	return string(ascii)
}
