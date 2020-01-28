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
	case 0x1DA:
		//Battery Current and Voltage
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}

		var motorAmps, motorSpd int16
		if frame.Data[2]&0b00001000 == 0b00001000 {
			motorAmps = int16((uint16(frame.Data[2]&0b00001111) << 8) | 0b1111000000000000 | (uint16(frame.Data[3])))
		} else {
			motorAmps = int16((uint16(frame.Data[2]&0b00001111)<<8)&0b0000111111111111 | (uint16(frame.Data[3])))
		}

		//
		motorSpd = int16((uint16(frame.Data[4]) << 8) | (uint16(frame.Data[5])))
		//
		//fmt.Println("Amps", motorAmps, "Speed", motorSpd)

		//motorAmps := (int16(frame.Data[2]) << 8) | (int16(frame.Data[3]))

		p := packetPool.Get().(*packet)
		ts := time.Now().UnixNano() / int64(time.Millisecond)
		p.sample.TimestampMs = ts
		p.sample.Value = float64(motorAmps)
		l := labelPool.Get().(labels.Label)
		l.Name = name
		l.Value = "motor_current"
		p.labels = append(p.labels, l)

		h.cortex.data <- p

		//motorSpd := (uint16(frame.Data[4]) << 8) | (uint16(frame.Data[5]))

		p1 := packetPool.Get().(*packet)
		p1.sample.TimestampMs = ts
		p1.sample.Value = float64(motorSpd)
		l1 := labelPool.Get().(labels.Label)
		l1.Name = name
		l1.Value = "motor_speed"
		p1.labels = append(p1.labels, l1)

		h.cortex.data <- p1
	case 0x1DB:
		//Battery Current and Voltage
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}

		// Even though the doc says the LSB for current is 0.5 it seems to reflect the actual charger current
		// more accurately when I don't ignore the last bit
		var battCurrent int16
		if frame.Data[0]&0b10000000 == 0b10000000 {
			battCurrent = int16((uint16(frame.Data[0]) << 3) | 0b1111100000000000 | uint16(frame.Data[1]>>6))
		} else {
			battCurrent = int16((uint16(frame.Data[0])<<3)&0b0000011111111111 | uint16(frame.Data[1]>>6))
		}

		// The voltage however seems to be more accurate when i do throw away the LSB (the doc would have us
		// shift left here 2 and add 3 from the second byte however that gave me 700+ volts)
		currVoltage := (uint16(frame.Data[2]) << 1) | (uint16(frame.Data[3]&0b11000000) >> 7)

		p := packetPool.Get().(*packet)
		ts := time.Now().UnixNano() / int64(time.Millisecond)
		p.sample.TimestampMs = ts
		p.sample.Value = float64(battCurrent)
		l := labelPool.Get().(labels.Label)
		l.Name = name
		l.Value = "battery_current"
		p.labels = append(p.labels, l)

		h.cortex.data <- p

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
