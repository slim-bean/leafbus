package push

import (
	"database/sql"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/brutella/can"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/slim-bean/leafbus/pkg/model"
	"github.com/slim-bean/leafbus/pkg/store"
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
	messagesStored = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "leafbus",
		Name:      "messages_stored_total",
		Help:      "Count of all messages from canbus stored locally.",
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
	store        *store.Writer
	runListeners []model.RunListener
	running      bool
	prevLights   uint8 //bit 7:  6:  5:  4:high  3: low  2: park  1: turnR  0: turnL
	lastGid      uint16
	tripStartGid uint16
	lastBatteryV float64
	streamMap    map[string][]*ratedFollower
	streamMtx    sync.Mutex
	statusMu     sync.Mutex
	status       store.StatusRow
}

func (h *Handler) Follow(name string, follower *stream.Follower) {
	h.streamMtx.Lock()
	defer h.streamMtx.Unlock()
	if _, ok := h.streamMap[name]; ok {
		for i := range h.streamMap[name] {
			if h.streamMap[name][i].Follower == follower {
				log.Println("ERROR, stream is already being followed with this channel")
				return
			}
		}
		log.Printf("New follower registered for: %v, count: %v\n", name, len(h.streamMap[name]))
		f := &ratedFollower{
			Follower: follower,
			lastSent: 0,
		}
		h.streamMap[name] = append(h.streamMap[name], f)
	} else {
		log.Println("First follower registered for: ", name)
		f := &ratedFollower{
			Follower: follower,
			lastSent: 0,
		}
		h.streamMap[name] = []*ratedFollower{f}
	}
}

func (h *Handler) Unfollow(name string, follower *stream.Follower) {
	h.streamMtx.Lock()
	defer h.streamMtx.Unlock()
	if _, ok := h.streamMap[name]; !ok {
		log.Println("ERROR, tried to unfollow a stream not being followed")
		return
	} else {
		for i := range h.streamMap[name] {
			if h.streamMap[name][i].Follower == follower {
				h.streamMap[name][i] = h.streamMap[name][len(h.streamMap[name])-1]
				h.streamMap[name][len(h.streamMap[name])-1] = nil
				h.streamMap[name] = h.streamMap[name][:len(h.streamMap[name])-1]
				log.Printf("Removed follower for metric %v, %v remaining followers", name, len(h.streamMap[name]))
				if len(h.streamMap[name]) == 0 {
					log.Printf("No longer following any streams for metric %v, removing\n", name)
					delete(h.streamMap, name)
				}
				return
			}
		}
		log.Printf("ERROR: Failed to remove follower for %v, did not find any matching channels", name)
	}
}

type ratedFollower struct {
	*stream.Follower
	lastSent int64
}

func NewHandler(writer *store.Writer) (*Handler, error) {
	h := &Handler{
		store:        writer,
		runListeners: []model.RunListener{},
		running:      false,
		prevLights:   0,
		streamMap:    map[string][]*ratedFollower{},
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
		steering := int16(uint16(frame.Data[1])<<8 | uint16(frame.Data[0]))
		ts := time.Now()
		h.SendMetric("steering_position", nil, ts, float64(steering))
	case 0x11A:
		// Gear and Key/Off/On
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
		speed := float64(uint16(frame.Data[4])<<8 | uint16(frame.Data[5]))
		speed = speed * 0.0062
		ts := time.Now()
		h.SendMetric("speed_mph", nil, ts, speed)

	case 0x292:
		// Friction Brake Pressure
		brake := frame.Data[6]
		ts := time.Now()
		h.SendMetric("friction_brake_pressure", nil, ts, float64(brake))
	case 0x358:
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
		ccPower := float64(frame.Data[3] >> 1 & 0b00111111)
		ccPower = ccPower * 0.25
		ts := time.Now()
		h.SendMetric("climate_control_kw", nil, ts, ccPower)
		h.SendMetric("climate_control_amps", nil, ts, ccPower/h.lastBatteryV)
	case 0x55B:
		//SOC
		currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
		h.SendMetric("soc", nil, time.Now(), float64(currCharge)/10)
		h.UpdateTractionSOC(time.Now(), float64(currCharge)/10)
	case 0x5B3:
		//GID
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
		odo := uint32(frame.Data[1])<<16 | uint32(frame.Data[2])<<8 | uint32(frame.Data[3])
		ts := time.Now()
		h.SendMetric("odometer", nil, ts, float64(odo))
	case 0x625:
		// Headlights
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

func (h *Handler) SendMetric(metricName string, additionalLabels labels.Labels, timestamp time.Time, val float64) {
	messagesStored.Inc()
	h.publishMetric(metricName, timestamp, val)
	if h.store == nil || !h.running {
		return
	}
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
	var labelsString sql.NullString
	if len(ls) > 0 {
		labelsString = nullString(ls.String())
	}
	h.store.EnqueueRuntime(store.RuntimeRow{
		Timestamp: timestamp,
		Name:      metricName,
		Value:     nullFloat(val),
		Text:      sql.NullString{},
		Labels:    labelsString,
		Kind:      nullString("metric"),
	})
}

func (h *Handler) SendLog(labels labels.Labels, timestamp time.Time, entry string) {
	if h.store == nil {
		return
	}
	name := labels.Get("job")
	if name == "" {
		name = "log"
	}
	var labelString sql.NullString
	if len(labels) > 0 {
		labelString = nullString(labels.String())
	}
	h.store.EnqueueRuntime(store.RuntimeRow{
		Timestamp: timestamp,
		Name:      name,
		Value:     sql.NullFloat64{},
		Text:      nullString(entry),
		Labels:    labelString,
		Kind:      nullString("log"),
	})
}

func (h *Handler) UpdateTractionSOC(ts time.Time, soc float64) {
	h.updateStatus(ts, func(s *store.StatusRow) {
		s.TractionSOC = nullFloat(soc)
	})
}

func (h *Handler) UpdateGPS(ts time.Time, lat float64, lon float64) {
	h.updateStatus(ts, func(s *store.StatusRow) {
		s.GPSLat = nullFloat(lat)
		s.GPSLon = nullFloat(lon)
	})
}

func (h *Handler) UpdateCharger(ts time.Time, state string, soc float64) {
	h.updateStatus(ts, func(s *store.StatusRow) {
		s.ChargerState = nullString(state)
		s.ChargerSOC = nullFloat(soc)
	})
}

func (h *Handler) UpdateBattery12V(ts time.Time, soc float64, volts float64, amps float64, temps []float64, status string) {
	h.updateStatus(ts, func(s *store.StatusRow) {
		s.Battery12VSOC = nullFloat(soc)
		s.Battery12VVolts = nullFloat(volts)
		s.Battery12VAmps = nullFloat(amps)
		s.Battery12VStatus = nullString(status)
		if len(temps) > 0 {
			sum := 0.0
			parts := make([]string, 0, len(temps))
			for _, t := range temps {
				sum += t
				parts = append(parts, fmtFloat(t))
			}
			s.Battery12VTempC = nullFloat(sum / float64(len(temps)))
			s.Battery12VTemps = nullString(strings.Join(parts, ","))
		}
	})
}

func (h *Handler) updateStatus(ts time.Time, updater func(*store.StatusRow)) {
	if h.store == nil {
		return
	}
	h.statusMu.Lock()
	defer h.statusMu.Unlock()
	if ts.IsZero() {
		h.status.Timestamp = time.Now().UTC()
	} else {
		h.status.Timestamp = ts.UTC()
	}
	updater(&h.status)
	h.store.EnqueueStatus(h.status)
}

func (h *Handler) publishMetric(name string, timestamp time.Time, val float64) {
	h.streamMtx.Lock()
	defer h.streamMtx.Unlock()
	followers := h.streamMap[name]
	if len(followers) == 0 {
		return
	}
	for _, f := range followers {
		if len(f.Follower.Pub) >= 1 {
			continue
		}
		ts := timestamp.UnixNano() / int64(time.Millisecond)
		if f.Follower.Rate > 0 {
			if ts-f.lastSent < f.Follower.Rate {
				continue
			}
			f.lastSent = ts
		}
		d := stream.GetData()
		d.Name = name
		d.Timestamp = ts
		d.Val = val
		f.Follower.Pub <- d
	}
}

func nullFloat(val float64) sql.NullFloat64 {
	return sql.NullFloat64{
		Float64: val,
		Valid:   true,
	}
}

func nullString(val string) sql.NullString {
	return sql.NullString{
		String: val,
		Valid:  true,
	}
}

func fmtFloat(val float64) string {
	return strconv.FormatFloat(val, 'f', 2, 64)
}
