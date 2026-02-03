package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/brutella/can"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/slim-bean/leafbus/pkg/charge"
	"github.com/slim-bean/leafbus/pkg/gps"
	"github.com/slim-bean/leafbus/pkg/heater"
	"github.com/slim-bean/leafbus/pkg/heaterui"
	"github.com/slim-bean/leafbus/pkg/hydra"
	"github.com/slim-bean/leafbus/pkg/ms4525"
	"github.com/slim-bean/leafbus/pkg/push"
	"github.com/slim-bean/leafbus/pkg/store"
	"github.com/slim-bean/leafbus/pkg/stream"
	"github.com/slim-bean/leafbus/pkg/wattcycle"
)

func main() {
	parquetDir := flag.String("parquet-dir", "", "Base directory for parquet output (required)")
	duckdbPath := flag.String("duckdb-path", "", "Optional path to the duckdb database file")
	wattcycleAddress := flag.String("wattcycle-address", wattcycle.DefaultAddress, "BLE address for the WattCycle 12V battery")
	heaterEnabled := flag.Bool("heater", true, "Enable battery heater control")
	heaterGPIO := flag.Int("heater-gpio", 17, "GPIO pin (BCM) for battery heater")
	heaterOnBelow := flag.Float64("heater-on-below", 35.0, "Heater ON when min temp <= value (F)")
	heaterOffAbove := flag.Float64("heater-off-above", 37.0, "Heater OFF when min temp >= value (F)")
	heaterActiveHigh := flag.Bool("heater-active-high", true, "Set GPIO high to turn heater on")
	flag.Parse()

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
	chargeMonitor, err := charge.NewMonitor("http://172.20.31.75", nil)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Creating handler")
	if *parquetDir == "" {
		log.Fatal("parquet-dir is required")
	}
	writer, err := store.NewWriter(*parquetDir, *duckdbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer writer.Close()

	handler, err := push.NewHandler(writer)
	if err != nil {
		log.Fatal(err)
	}
	chargeMonitor.SetHandler(handler)

	log.Println("Creating GPS")
	gps, err := gps.NewGPS(handler, "/dev/ttyAMA3")
	if err != nil {
		log.Fatal(err)
	}

	// log.Println("Creatign Cam")
	// cam, err := cam.NewCam(handler)
	// if err != nil {
	// 	log.Fatal(err)
	// }

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

	log.Println("Creating WattCycle monitor")
	wattMonitor, err := wattcycle.NewMonitor(wattcycle.Config{
		Address: *wattcycleAddress,
	})
	var heaterCtrl *heater.Controller
	var heaterCtrlErr error
	if err != nil {
		log.Println("Failed to create WattCycle monitor:", err)
	} else {
		if err := wattMonitor.Start(); err != nil {
			log.Println("Failed to start WattCycle monitor:", err)
		} else {
			if *heaterEnabled {
				activeHigh := *heaterActiveHigh
				heaterCtrl, err = heater.NewController(heater.Config{
					GPIO:       *heaterGPIO,
					OnBelowC:   fToC(*heaterOnBelow),
					OffAboveC:  fToC(*heaterOffAbove),
					ActiveHigh: &activeHigh,
				})
				if err != nil {
					heaterCtrlErr = err
					log.Println("Failed to create heater controller:", err)
				} else {
					log.Printf("Heater controller active (GPIO %d, on<=%.1fF, off>=%.1fF)\n", *heaterGPIO, *heaterOnBelow, *heaterOffAbove)
				}
			} else {
				heaterCtrlErr = fmt.Errorf("heater disabled (enable with -heater)")
			}
			go func() {
				for st := range wattMonitor.Statuses() {
					handler.UpdateBattery12V(st.Timestamp, st.SOC, st.Voltage, st.Current, st.TempsC, st.Status)
					if heaterCtrl != nil {
						heaterCtrl.UpdateTemps(st.TempsC)
						heaterStatus := heaterCtrl.Status()
						handler.UpdateHeater(st.Timestamp, heaterStatus.Mode, heaterStatus.On, heaterStatus.ManualOn, heaterStatus.MinTempC)
					}
				}
			}()
		}
	}

	handler.RegisterRunListener(ms)
	handler.RegisterRunListener(gps)
	//handler.RegisterRunListener(cam)

	log.Println("Creating new Bus and subscribing")
	bus0 := can.NewBus(conn0)
	bus0.SubscribeFunc(chargeMonitor.Handle)
	bus0.SubscribeFunc(handler.Handle)
	bus1 := can.NewBus(conn1)
	bus1.SubscribeFunc(handler.Handle)

	log.Println("Starting web server")
	http.HandleFunc("/stream", strm.Handler)
	http.HandleFunc("/query", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload queryRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(response, "invalid JSON body", http.StatusBadRequest)
			return
		}
		payload.SQL = strings.TrimSpace(payload.SQL)
		if payload.SQL == "" {
			http.Error(response, "sql is required", http.StatusBadRequest)
			return
		}
		if !isQueryAllowed(payload.SQL) {
			http.Error(response, "only SELECT/WITH queries are allowed", http.StatusBadRequest)
			return
		}
		sqlQuery := applyLimit(payload.SQL, payload.Limit)
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		result, err := writer.Query(ctx, sqlQuery)
		if err != nil {
			http.Error(response, fmt.Sprintf("query failed: %v", err), http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(response).Encode(result); err != nil {
			log.Println("failed to write query response:", err)
		}
	})
	heaterui.Register(http.DefaultServeMux, handler, func() (*heater.Controller, error) {
		if heaterCtrl != nil {
			return heaterCtrl, nil
		}
		return nil, heaterCtrlErr
	})
	http.HandleFunc("/control", func(writer http.ResponseWriter, request *http.Request) {
		run := request.URL.Query().Get("run")
		if strings.ToLower(run) == "true" {
			log.Println("Starting Services from HTTP Request")
			ms.Start()
			gps.Start()
			//cam.Start()
			writer.WriteHeader(http.StatusOK)
			return
		} else if strings.ToLower(run) == "false" {
			log.Println("Stopping Services from HTTP Request")
			ms.Stop()
			gps.Stop()
			//cam.Stop()
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
		if wattMonitor != nil {
			wattMonitor.Stop()
		}
		if heaterCtrl != nil {
			heaterCtrl.Close()
		}
	}
	log.Println("Exiting")
}

type queryRequest struct {
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
}

func isQueryAllowed(sql string) bool {
	if strings.Contains(sql, ";") {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(sql))
	return strings.HasPrefix(normalized, "select ") || strings.HasPrefix(normalized, "with ")
}

func applyLimit(sql string, limit int) string {
	if limit <= 0 {
		return sql
	}
	lowered := strings.ToLower(sql)
	if strings.Contains(lowered, " limit ") {
		return sql
	}
	return fmt.Sprintf("%s limit %d", strings.TrimSpace(sql), limit)
}

func fToC(tempF float64) float64 {
	return (tempF - 32.0) * 5.0 / 9.0
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
