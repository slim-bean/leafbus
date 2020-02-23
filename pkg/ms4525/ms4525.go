package ms4525

import (
	"fmt"
	_ "net/http/pprof"
	"time"

	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"

	"github.com/slim-bean/leafbus/pkg/push"
)

const (
	pmax      = 1.0
	pmin      = -1.0
	TempName  = "outside_temp"
	PressName = "air_pressure"
)

type MS4525 struct {
	handler   *push.Handler
	bus       *i2c.I2C
	runChan   chan bool
	shouldRun bool
}

func NewMS4525(handler *push.Handler, bus int) (*MS4525, error) {
	logger.ChangePackageLogLevel("i2c", logger.InfoLevel)
	b, err := i2c.NewI2C(0x28, bus)
	if err != nil {
		return nil, err
	}

	ms := &MS4525{
		handler:   handler,
		bus:       b,
		shouldRun: false,
		runChan:   make(chan bool),
	}
	go ms.run()
	return ms, nil
}

func (m *MS4525) Start() {
	m.runChan <- true
}

func (m *MS4525) Stop() {
	m.runChan <- false
}

func (m *MS4525) run() {
	//Start condition
	start := make([]byte, 0)
	data := make([]byte, 4)
	ticker := time.NewTicker(10 * time.Millisecond)

	for {

		select {
		case <-ticker.C:
			if !m.shouldRun {
				continue
			}

			_, err := m.bus.ReadBytes(start)
			if err != nil {
				fmt.Println("Failed to send MS4525 start conversion:", err)
				time.Sleep(1 * time.Second)
				continue
			}
			//time.Sleep(10 * time.Millisecond)

			_, err = m.bus.ReadBytes(data)
			if err != nil {
				fmt.Println("Failed to read MS4525 data:", err)
				time.Sleep(1 * time.Second)
				continue
			}

			//ts := time.Now().UnixNano() / int64(time.Millisecond)
			ts := time.Now()
			//p := m.handler.GetPacket()
			//p.Sample.TimestampMs = ts
			//pressure := -((float64(uint16(data[0])<<8|uint16(data[1]))-0.1*16383)*(pmax-pmin)/(0.8*16383) + pmin)
			pressure := float64(uint16(data[0])<<8 | uint16(data[1]))
			m.handler.SendMetric(PressName, nil, ts, pressure)
			//m.handler.SendPacket(p, PressName)
			//-((dp_raw - 0.1f*16383) * (P_max-P_min)/(0.8f*16383) + P_min);

			//p1 := m.handler.GetPacket()
			//p1.Sample.TimestampMs = ts
			temp := ((200 * float64(uint16(data[2])<<3|uint16((data[3]&0b11100000)>>5))) / 2047) - 50
			//m.handler.SendPacket(p1, TempName)
			m.handler.SendMetric(TempName, nil, ts, temp)

			//((200.0f * dT_raw) / 2047) - 50
			//fmt.Printf("One: %v, Two: %v, Three: %v\n", strconv.FormatUint(uint64(data[0]), 2), strconv.FormatUint(uint64(data[1]), 2), strconv.FormatUint(uint64(data[2]), 2))
			//fmt.Printf("Pressure: %v, Temp: %v\n", pressure, temp)

		case r := <-m.runChan:
			m.shouldRun = r

		}

	}
}
