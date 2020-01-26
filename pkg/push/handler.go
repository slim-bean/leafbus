package push

import (
	"log"
	"time"

	"github.com/brutella/can"
	"github.com/prometheus/prometheus/pkg/labels"
)

const (
	name = "__name__"
)

type Handler struct {
	cortex *Cortex
}

func NewHandler(cortex *Cortex) (*Handler, error) {
	h := &Handler{
		cortex: cortex,
	}
	return h, nil
}

func (h *Handler) Handle(frame can.Frame) {
	// Only care about current charge status
	switch frame.ID {
	case 0x55B:
		//SOC
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}

		currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)

		p := packetPool.Get().(*packet)
		p.sample.TimestampMs = time.Now().UnixNano() / int64(time.Millisecond)
		p.sample.Value = float64(currCharge)
		l := labelPool.Get().(labels.Label)
		l.Name = name
		l.Value = "current_soc"
		p.labels = append(p.labels, l)

		h.cortex.data <- p
	case 0x1DB:
		//Battery Current and Voltage
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}

		currCharge := (int16(frame.Data[0]) << 3) | (int16(frame.Data[1]) >> 5)

		p := packetPool.Get().(*packet)
		ts := time.Now().UnixNano() / int64(time.Millisecond)
		p.sample.TimestampMs = ts
		p.sample.Value = float64(currCharge)
		l := labelPool.Get().(labels.Label)
		l.Name = name
		l.Value = "battery_current"
		p.labels = append(p.labels, l)

		h.cortex.data <- p

		currVoltage := (uint16(frame.Data[2]) << 1) | (uint16(frame.Data[3]) >> 6)

		p1 := packetPool.Get().(*packet)
		p1.sample.TimestampMs = ts
		p1.sample.Value = float64(currVoltage)
		l1 := labelPool.Get().(labels.Label)
		l1.Name = name
		l1.Value = "battery_voltage"
		p1.labels = append(p1.labels, l1)

		h.cortex.data <- p1
	}
}
