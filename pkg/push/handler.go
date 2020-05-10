package push

import (
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/brutella/can"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/slim-bean/leafbus/pkg/model"
	"github.com/slim-bean/leafbus/pkg/stream"
)

const (
	name = "__name__"
)

var (
	canMessages = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "leafbus",
		Name:      "can_messages_total",
		Help:      "Count of all messages received on canbus",
	})
	messagesSentCortex = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "leafbus",
		Name:      "messages_sent_total",
		Help:      "Count of all messages from canbus sent to Cortex.",
	})
	keyLabel = labels.Labels{
		labels.Label{
			Name:  "job",
			Value: "key",
		},
	}
	turnLabel = labels.Labels{
		labels.Label{
			Name:  "job",
			Value: "turn_signal",
		},
	}
	lightLabel = labels.Labels{
		labels.Label{
			Name:  "job",
			Value: "headlights",
		},
	}
)

type Handler struct {
	cortex       *cortex
	loki         *loki
	runListeners []model.RunListener
	running      bool
	prevLights   uint8 //bit 7:  6:  5:  4:high  3: low  2: park  1: turnR  0: turnL
	lastGid      uint16
	tripStartGid uint16
	lastBatteryV float64
}

func (h *Handler) Follow(name string, follower *stream.Follower) {
	h.cortex.follow(name, follower)
}

func (h *Handler) Unfollow(name string, follower *stream.Follower) {
	h.cortex.unfollow(name, follower)
}

func NewHandler(cortexAddress string, lokiAddress string) (*Handler, error) {
	c, err := newCortex(cortexAddress)
	if err != nil {
		return nil, err
	}
	l, err := newLoki(lokiAddress)
	if err != nil {
		return nil, err
	}
	h := &Handler{
		cortex:       c,
		loki:         l,
		runListeners: []model.RunListener{},
		running:      false,
		prevLights:   0,
	}
	return h, nil
}

// TODO this is not thread safe
func (h *Handler) RegisterRunListener(rl model.RunListener) {
	h.runListeners = append(h.runListeners, rl)
}

func (h *Handler) Handle(frame can.Frame) {
	canMessages.Inc()
	switch frame.ID {

	case 0x002:
		// Steering Position
		if h.metricBufferFull() {
			return
		}
		steering := int16(uint16(frame.Data[1])<<8 | uint16(frame.Data[0]))
		ts := time.Now()
		h.SendMetric("steering_position", nil, ts, float64(steering))
	case 0x11A:
		// Gear and Key/Off/On
		if h.logBufferFull() {
			return
		}
		keyOn := false
		if frame.Data[1]&0b11100000 == 0b10000000 {
			// Off
			keyOn = false
		} else if frame.Data[1]&0b11100000 == 0b01000000 {
			// On
			keyOn = true
		}
		if keyOn && !h.running {
			// Key is on, but not running, start
			for _, l := range h.runListeners {
				l.Start()
			}
			h.running = true
			h.SendLog(keyLabel, time.Now(), "Key Turned On")
			h.tripStartGid = h.lastGid
		} else if !keyOn && h.running {
			// Key is off, currently running, stop
			for _, l := range h.runListeners {
				l.Stop()
			}
			h.running = false
			h.SendLog(keyLabel, time.Now(), "Key Turned Off")
		}
	case 0x180:
		//Throttle Position and Motor Amps
		if h.metricBufferFull() {
			return
		}
		motorAmps := (uint16(frame.Data[2]) << 4) | (uint16(frame.Data[3]) >> 4)
		ts := time.Now()
		h.SendMetric("motor_amps", nil, ts, float64(motorAmps))
		throttle := float64((uint16(frame.Data[5]) << 2) | (uint16(frame.Data[6]) >> 6))
		throttle = (throttle / 800) * 100
		h.SendMetric("throttle_percent", nil, ts, throttle)
	case 0x1CB:
		// Target brake position
		brake := (uint16(frame.Data[2]) << 2) | (uint16(frame.Data[3]) >> 6)
		ts := time.Now()
		h.SendMetric("target_brake", nil, ts, float64(brake))
	case 0x1DA:
		//Motor Torque and Speed
		if h.metricBufferFull() {
			return
		}
		var effectiveTorque int16
		if frame.Data[2]&0b00000100 == 0b00000100 {
			effectiveTorque = int16(((uint16(frame.Data[2]&0b00000111) << 8) | 0b1111100000000000) | uint16(frame.Data[3]))
		} else {
			effectiveTorque = int16(((uint16(frame.Data[2]&0b00000111) << 8) & 0b0000011111111111) | uint16(frame.Data[3]))
		}
		motorSpeed := int16(uint16(frame.Data[4])<<8 | uint16(frame.Data[5]))
		ts := time.Now()
		h.SendMetric("effective_torque", nil, ts, float64(effectiveTorque))
		h.SendMetric("motor_rpm", nil, ts, float64(motorSpeed))

	case 0x1DB:
		//Battery Current and Voltage
		if h.metricBufferFull() {
			return
		}
		var battCurrent int16
		if frame.Data[0]&0b10000000 == 0b10000000 {
			battCurrent = int16((uint16(frame.Data[0]) << 3) | 0b1111100000000000 | uint16(frame.Data[1]>>6))
		} else {
			battCurrent = int16((uint16(frame.Data[0])<<3)&0b0000011111111111 | uint16(frame.Data[1]>>6))
		}
		currVoltage := float64((uint16(frame.Data[2]) << 2) | (uint16(frame.Data[3]&0b11000000) >> 6))
		currVoltage = currVoltage * 0.5
		ts := time.Now()
		// Invert the battery current reading because I prefer it this way
		h.SendMetric("battery_amps", nil, ts, float64(-battCurrent))
		h.SendMetric("battery_volts", nil, ts, float64(currVoltage))
		h.lastBatteryV = currVoltage
	case 0x280:
		// Speed
		if h.metricBufferFull() {
			return
		}
		speed := float64(uint16(frame.Data[4])<<8 | uint16(frame.Data[5]))
		speed = speed * 0.0062
		ts := time.Now()
		h.SendMetric("speed_mph", nil, ts, speed)

	case 0x292:
		// Friction Brake Pressure
		if h.metricBufferFull() {
			return
		}
		brake := frame.Data[6]
		ts := time.Now()
		h.SendMetric("friction_brake_pressure", nil, ts, float64(brake))
	case 0x358:
		if h.logBufferFull() {
			return
		}
		// Turn Signal
		turnL := frame.Data[2]&0b00000010 == 0b00000010
		if turnL && h.prevLights&0b00000001 != 0b000000001 {
			ts := time.Now()
			h.SendLog(turnLabel, ts, "Left Turn Signal On")
			h.prevLights |= 0b00000001
		} else if !turnL && h.prevLights&0b00000001 != 0b00000000 {
			ts := time.Now()
			h.SendLog(turnLabel, ts, "Left Turn Signal Off")
			h.prevLights &= 0b11111110
		}
		turnR := frame.Data[2]&0b00000100 == 0b00000100
		if turnR && h.prevLights&0b00000010 != 0b000000010 {
			ts := time.Now()
			h.SendLog(turnLabel, ts, "Right Turn Signal On")
			h.prevLights |= 0b00000010
		} else if !turnR && h.prevLights&0b00000010 != 0b00000000 {
			ts := time.Now()
			h.SendLog(turnLabel, ts, "Right Turn Signal Off")
			h.prevLights &= 0b11111101
		}
	case 0x510:
		//Climate control power
		if h.metricBufferFull() {
			return
		}
		ccPower := float64(frame.Data[3] >> 1 & 0b00111111)
		ccPower = ccPower * 0.25
		ts := time.Now()
		h.SendMetric("climate_control_kw", nil, ts, ccPower)
		h.SendMetric("climate_control_amps", nil, ts, ccPower/h.lastBatteryV)
	case 0x55B:
		//SOC
		if h.metricBufferFull() {
			return
		}
		currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
		h.SendMetric("soc", nil, time.Now(), float64(currCharge)/10)
	case 0x5B3:
		//GID
		if h.metricBufferFull() {
			return
		}
		gid := uint16(frame.Data[4]&0b00000001)<<8 | uint16(frame.Data[5])
		// Sometimes we get a bogus gid value of 511 so just send the last value
		if gid == 511 {
			gid = h.lastGid
		} else {
			h.lastGid = gid
		}
		ts := time.Now()
		h.SendMetric("gids", nil, ts, float64(gid))
		h.SendMetric("trip_gids", nil, ts, float64(h.tripStartGid-gid))
	case 0x5C5:
		//Odometer
		if h.metricBufferFull() {
			return
		}
		odo := uint32(frame.Data[1])<<16 | uint32(frame.Data[2])<<8 | uint32(frame.Data[3])
		ts := time.Now()
		h.SendMetric("odometer", nil, ts, float64(odo))
	case 0x625:
		// Headlights
		if h.logBufferFull() {
			return
		}
		parkL := frame.Data[1]&0b01000000 == 0b01000000
		if parkL && h.prevLights&0b00000100 != 0b000000100 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "Parking Lights On")
			h.prevLights |= 0b00000100
		} else if !parkL && h.prevLights&0b00000100 != 0b00000000 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "Parking Lights Off")
			h.prevLights &= 0b11111011
		}
		lowBeam := frame.Data[1]&0b00100000 == 0b00100000
		if lowBeam && h.prevLights&0b00001000 != 0b000001000 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "Low Beams On")
			h.prevLights |= 0b00001000
		} else if !lowBeam && h.prevLights&0b00001000 != 0b00000000 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "Low Beams Off")
			h.prevLights &= 0b11110111
		}
		highBeam := frame.Data[1]&0b00010000 == 0b00010000
		if highBeam && h.prevLights&0b00010000 != 0b000010000 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "High Beams On")
			h.prevLights |= 0b00010000
		} else if !highBeam && h.prevLights&0b00010000 != 0b00000000 {
			ts := time.Now()
			h.SendLog(lightLabel, ts, "High Beams Off")
			h.prevLights &= 0b11101111
		}

	}
}

func (h *Handler) metricBufferFull() bool {
	if len(h.cortex.data) >= 100 {
		log.Println("Ignoring packet, send buffer is full")
		return true
	}
	return false
}

func (h *Handler) logBufferFull() bool {
	if len(h.loki.data) >= 100 {
		log.Println("Ignoring packet, send buffer is full")
		return true
	}
	return false
}

func (h *Handler) SendMetric(metricName string, additionalLabels labels.Labels, timestamp time.Time, val float64) {
	messagesSentCortex.Inc()
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

func (h *Handler) SendLog(labels labels.Labels, timestamp time.Time, entry string) {
	h.loki.data <- &singleLog{
		Labels: labels,
		Entry: &logproto.Entry{
			Timestamp: timestamp,
			Line:      entry,
		},
	}
}
