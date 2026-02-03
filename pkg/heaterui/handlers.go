package heaterui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/slim-bean/leafbus/pkg/heater"
	"github.com/slim-bean/leafbus/pkg/push"
)

//go:embed heater.html
var heaterPage string

type heaterControlRequest struct {
	Mode     string `json:"mode"`
	ManualOn *bool  `json:"manual_on"`
}

type heaterStatusResponse struct {
	Heater  *heater.Status   `json:"heater,omitempty"`
	Battery *batterySnapshot `json:"battery,omitempty"`
	Error   string           `json:"error,omitempty"`
}

type batterySnapshot struct {
	Timestamp     time.Time `json:"timestamp"`
	SOC           float64   `json:"soc"`
	Volts         float64   `json:"volts"`
	Amps          float64   `json:"amps"`
	TempC         float64   `json:"temp_c"`
	TempsC        []float64 `json:"temps_c,omitempty"`
	Status        string    `json:"status"`
	HasTemps      bool      `json:"has_temps"`
	HasVoltage    bool      `json:"has_voltage"`
	HasCurrent    bool      `json:"has_current"`
	HasSOC        bool      `json:"has_soc"`
	HasStatusText bool      `json:"has_status_text"`
}

func Register(mux *http.ServeMux, handler *push.Handler, heaterProvider func() (*heater.Controller, error)) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	if heaterProvider == nil {
		heaterProvider = func() (*heater.Controller, error) {
			return nil, errors.New("heater controller provider not configured")
		}
	}

	mux.HandleFunc("/heater", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			log.Printf("heater ui: invalid method %s for /heater", request.Method)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(heaterPage))
	})

	mux.HandleFunc("/heater/status", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			log.Printf("heater ui: invalid method %s for /heater/status", request.Method)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		heaterCtrl, err := heaterProvider()
		if err != nil || heaterCtrl == nil {
			log.Println("heater ui: heater controller not available for /heater/status:", err)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(writer).Encode(heaterStatusResponse{
				Error: "heater controller not available",
			})
			return
		}
		resp := heaterStatusResponse{
			Heater:  ptrHeaterStatus(heaterCtrl.Status()),
			Battery: buildBatterySnapshot(handler),
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(resp); err != nil {
			log.Println("heater ui: failed to write heater status response:", err)
		}
	})

	mux.HandleFunc("/heater/control", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			log.Printf("heater ui: invalid method %s for /heater/control", request.Method)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		heaterCtrl, err := heaterProvider()
		if err != nil || heaterCtrl == nil {
			log.Println("heater ui: heater controller not available for /heater/control:", err)
			writer.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(writer).Encode(heaterStatusResponse{
				Error: "heater controller not available",
			})
			return
		}
		var payload heaterControlRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			log.Println("heater ui: invalid control payload:", err)
			http.Error(writer, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if payload.Mode != "" {
			if err := heaterCtrl.SetMode(payload.Mode); err != nil {
				log.Println("heater ui: failed to set heater mode:", err)
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if payload.ManualOn != nil {
			if err := heaterCtrl.SetManualOn(*payload.ManualOn); err != nil {
				log.Println("heater ui: failed to set manual heater state:", err)
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
		}
		resp := heaterStatusResponse{
			Heater:  ptrHeaterStatus(heaterCtrl.Status()),
			Battery: buildBatterySnapshot(handler),
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(resp); err != nil {
			log.Println("heater ui: failed to write control response:", err)
		}
	})
}

func buildBatterySnapshot(handler *push.Handler) *batterySnapshot {
	if handler == nil {
		return nil
	}
	st, ok := handler.LatestStatus()
	if !ok {
		return nil
	}
	snap := &batterySnapshot{
		Timestamp: st.Timestamp,
	}
	if st.Battery12VSOC.Valid {
		snap.SOC = st.Battery12VSOC.Float64
		snap.HasSOC = true
	}
	if st.Battery12VVolts.Valid {
		snap.Volts = st.Battery12VVolts.Float64
		snap.HasVoltage = true
	}
	if st.Battery12VAmps.Valid {
		snap.Amps = st.Battery12VAmps.Float64
		snap.HasCurrent = true
	}
	if st.Battery12VTempC.Valid {
		snap.TempC = st.Battery12VTempC.Float64
		snap.HasTemps = true
	}
	if st.Battery12VTemps.Valid {
		snap.TempsC = parseTemps(st.Battery12VTemps.String)
	}
	if st.Battery12VStatus.Valid {
		snap.Status = st.Battery12VStatus.String
		snap.HasStatusText = true
	}
	return snap
}

func parseTemps(raw string) []float64 {
	parts := strings.Split(raw, ",")
	temps := make([]float64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		val, err := strconv.ParseFloat(part, 64)
		if err != nil {
			continue
		}
		temps = append(temps, val)
	}
	return temps
}

func ptrHeaterStatus(st heater.Status) *heater.Status {
	return &st
}
