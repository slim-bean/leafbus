package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/brutella/can"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/slim-bean/leafbus/pkg/cam"
	"github.com/slim-bean/leafbus/pkg/charge"
	"github.com/slim-bean/leafbus/pkg/gps"
	"github.com/slim-bean/leafbus/pkg/hydra"
	"github.com/slim-bean/leafbus/pkg/ms4525"
	"github.com/slim-bean/leafbus/pkg/push"
	"github.com/slim-bean/leafbus/pkg/stream"
)

func main() {
	cortexAddress := flag.String("cortex-address", "localhost:9002", "GRPC address and port to find cortex")
	lokiAddress := flag.String("loki-address", "localhost:9003", "GRPC address and port to find cortex")

	log.Println("Finding interface can0")
	iface0, err := net.InterfaceByName("can0")
	if err != nil {
		log.Fatalf("Could not find network interface %s (%v)", "can0", err)
	}
	log.Println("Opening interface can0")
	conn0, err := can.NewReadWriteCloserForInterface(iface0)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Finding interface can1")
	iface1, err := net.InterfaceByName("can1")
	if err != nil {
		log.Fatalf("Could not find network interface %s (%v)", "can1", err)
	}
	log.Println("Opening interface can1")
	conn1, err := can.NewReadWriteCloserForInterface(iface1)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating new Charge Monitor")
	chargeMonitor, err := charge.NewMonitor("http://172.20.31.75")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating handler")
	handler, err := push.NewHandler(*cortexAddress, *lokiAddress)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating GPS")
	gps, err := gps.NewGPS(handler, "/dev/ttyAMA0")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creatign Cam")
	cam, err := cam.NewCam(handler)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating streamer")
	strm := stream.NewStreamer(handler)

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

	log.Println("Creating MS4525")
	ms, err := ms4525.NewMS4525(handler, 1)
	if err != nil {
		log.Println(err)
	}

	handler.RegisterRunListener(ms)
	handler.RegisterRunListener(gps)
	handler.RegisterRunListener(cam)

	log.Println("Creating new Bus and subscribing")
	bus0 := can.NewBus(conn0)
	bus0.SubscribeFunc(chargeMonitor.Handle)
	bus0.SubscribeFunc(handler.Handle)
	bus1 := can.NewBus(conn1)
	bus1.SubscribeFunc(handler.Handle)

	log.Println("Starting web server")
	http.HandleFunc("/stream", strm.Handler)
	http.HandleFunc("/control", func(writer http.ResponseWriter, request *http.Request) {
		run := request.URL.Query().Get("run")
		if strings.ToLower(run) == "true" {
			log.Println("Starting Services from HTTP Request")
			ms.Start()
			gps.Start()
			cam.Start()
			writer.WriteHeader(http.StatusOK)
			return
		} else if strings.ToLower(run) == "false" {
			log.Println("Stopping Services from HTTP Request")
			ms.Stop()
			gps.Stop()
			cam.Stop()
			writer.WriteHeader(http.StatusOK)
			return
		}
		writer.WriteHeader(http.StatusBadRequest)
	})
	// Expose the registered metrics via HTTP.
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	go func() {
		if err := http.ListenAndServe(":7777", nil); err != nil {
			log.Println(err)
		}
	}()

	log.Println("Listen on Can Buses")
	go func() {
		err = bus0.ConnectAndPublish()
		if err != nil {
			log.Println(err)
		}
	}()
	go func() {
		err = bus1.ConnectAndPublish()
		if err != nil {
			log.Println(err)
		}
	}()

	log.Println("Wait for sigint or kill")
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)

	select {
	case <-c:
		bus0.Disconnect()
		bus1.Disconnect()
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
