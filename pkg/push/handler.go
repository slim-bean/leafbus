package push

import (
	"log"
	"time"

	"github.com/brutella/can"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/slim-bean/leafbus/pkg/stream"
)

const (
	name = "__name__"
)

type Handler struct {
	cortex *cortex
}

func (h *Handler) Follow(name string, follower *stream.Follower) {
	h.cortex.follow(name, follower)
}

func (h *Handler) Unfollow(name string, follower *stream.Follower) {
	h.cortex.unfollow(name, follower)
}

func NewHandler(address string) (*Handler, error) {
	c, err := newCortex(address)
	if err != nil {
		return nil, err
	}
	h := &Handler{
		cortex: c,
	}
	return h, nil
}

func (h *Handler) Handle(frame can.Frame) {

	switch frame.ID {
	case 0x55B:
		//SOC
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}
		currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
		h.SendMetric("soc", nil, time.Now(), float64(currCharge)/10)

	case 0x1DA:
		//Battery Current and Voltage
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}
		var motorAmps int16
		if frame.Data[2]&0b00000100 == 0b00000100 {
			motorAmps = int16(((uint16(frame.Data[2]&0b00000111) << 8) | 0b1111100000000000) | uint16(frame.Data[3]))
		} else {
			motorAmps = int16(((uint16(frame.Data[2]&0b00000111) << 8) & 0b0000011111111111) | uint16(frame.Data[3]))
		}
		motorSpeed := int16(uint16(frame.Data[4])<<8 | uint16(frame.Data[5]))
		ts := time.Now()
		h.SendMetric("motor_amps", nil, ts, float64(motorAmps))
		h.SendMetric("motor_rpm", nil, ts, float64(motorSpeed))

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
		ts := time.Now()
		h.SendMetric("battery_amps", nil, ts, float64(battCurrent))
		h.SendMetric("battery_volts", nil, ts, float64(currVoltage))
	case 0x5BC:
		//GID
		if len(h.cortex.data) >= 100 {
			log.Println("Ignoring packet, send buffer is full")
			return
		}
		gid := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
		ts := time.Now()
		h.SendMetric("gid", nil, ts, float64(gid))

	}
}

func (h *Handler) SendMetric(metricName string, additionalLabels labels.Labels, timestamp time.Time, val float64) {
	//p := packetPool.Get().(*Packet)
	l := labels.Label{
		Name:  name,
		Value: metricName,
	}
	var ls labels.Labels
	if additionalLabels != nil {
		ls = make(labels.Labels, 0, len(additionalLabels)+1)
		ls = append(ls, l)
		for _, al := range additionalLabels {
			ls = append(ls, al)
		}
	} else {
		ls = labels.Labels{l}
	}

	p := &Packet{
		Labels: ls,
		Sample: client.Sample{
			TimestampMs: timestamp.UnixNano() / int64(time.Millisecond),
			Value:       val,
		},
	}
	//ts := timestamp.UnixNano() / int64(time.Millisecond)
	//p.Sample.TimestampMs = ts
	//p.Sample.Value = val
	////l := labelPool.Get().(labels.Label)
	//labels.New()
	//l.Name = name
	//l.Value = metricName
	//p.Labels = append(p.Labels, l)
	//if additionalLabels != nil {
	//	p.Labels = append(p.Labels, additionalLabels...)
	//}
	h.cortex.data <- p
}
