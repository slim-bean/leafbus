package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
		sqlQuery, limit, err := parseQueryRequest(request)
		if err != nil {
			writeQueryError(response, http.StatusBadRequest, err.Error(), sqlQuery)
			return
		}
		sqlQuery = strings.TrimSpace(sqlQuery)
		if sqlQuery == "" {
			writeQueryError(response, http.StatusBadRequest, "sql is required", sqlQuery)
			return
		}
		if !isQueryAllowed(sqlQuery) {
			writeQueryError(response, http.StatusBadRequest, "only SELECT/WITH queries are allowed", sqlQuery)
			return
		}
		sqlQuery = applyLimit(sqlQuery, limit)
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		result, err := writer.Query(ctx, sqlQuery)
		if err != nil {
			writeQueryError(response, http.StatusBadRequest, fmt.Sprintf("query failed: %v", err), sqlQuery)
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

type queryErrorResponse struct {
	Error string `json:"error"`
	SQL   string `json:"sql,omitempty"`
}

func writeQueryError(response http.ResponseWriter, status int, message string, sql string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(queryErrorResponse{
		Error: message,
		SQL:   strings.TrimSpace(sql),
	})
}

func parseQueryRequest(request *http.Request) (string, int, error) {
	sqlQuery := strings.TrimSpace(request.URL.Query().Get("sql"))
	limit, err := parseQueryLimit(request)
	if err != nil {
		return sqlQuery, 0, err
	}
	if sqlQuery != "" {
		return sqlQuery, limit, nil
	}
	if request.Method != http.MethodPost {
		return "", 0, fmt.Errorf("use POST with JSON or text body, or provide ?sql=")
	}
	contentType := request.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var payload queryRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return "", 0, fmt.Errorf("invalid JSON body")
		}
		payload.SQL = strings.TrimSpace(payload.SQL)
		if limit <= 0 {
			limit = payload.Limit
		}
		return payload.SQL, limit, nil
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read request body")
	}
	return strings.TrimSpace(string(body)), limit, nil
}

func parseQueryLimit(request *http.Request) (int, error) {
	limitRaw := strings.TrimSpace(request.URL.Query().Get("limit"))
	if limitRaw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(limitRaw)
	if err != nil {
		return 0, fmt.Errorf("limit must be an integer")
	}
	if limit < 0 {
		return 0, fmt.Errorf("limit must be >= 0")
	}
	return limit, nil
}

func isQueryAllowed(sql string) bool {
	normalized := strings.TrimSpace(strings.ToLower(sql))
	if strings.Contains(normalized, ";") {
		return false
	}
	return hasSQLPrefix(normalized, "select") || hasSQLPrefix(normalized, "with")
}

func hasSQLPrefix(sql string, keyword string) bool {
	if !strings.HasPrefix(sql, keyword) {
		return false
	}
	if len(sql) == len(keyword) {
		return true
	}
	next := sql[len(keyword)]
	return next == ' ' || next == '\n' || next == '\t' || next == '\r'
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
