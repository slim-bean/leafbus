package charge

import (
	"log"
	"sync"
	"time"

	"github.com/brutella/can"
)

type battCurrent struct {
	current int16
	time    time.Time
}

type Monitor struct {
	charger     *openevse
	currCharge  uint16
	battCurrent battCurrent
	time        time.Time
	mtx         sync.Mutex
}

func NewMonitor(chargerAddress string) (*Monitor, error) {
	ch, err := newopenevse(chargerAddress)
	if err != nil {
		return nil, err
	}
	m := &Monitor{
		charger: ch,
	}
	go m.run()
	return m, nil
}

func (m *Monitor) Handle(frame can.Frame) {
	// Only care about current charge status
	switch frame.ID {
	case 0x55B:
		m.mtx.Lock()
		m.currCharge = (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
		m.mtx.Unlock()
	case 0x1DB:
		//Battery Current and Voltage
		var battCurrent int16
		if frame.Data[0]&0b10000000 == 0b10000000 {
			battCurrent = int16((uint16(frame.Data[0]) << 3) | 0b1111100000000000 | uint16(frame.Data[1]>>6))
		} else {
			battCurrent = int16((uint16(frame.Data[0])<<3)&0b0000011111111111 | uint16(frame.Data[1]>>6))
		}
		m.mtx.Lock()
		m.battCurrent.current = battCurrent
		// We don't need a lot of accuracy for this time so we use a time value updated every 10 seconds
		// in the run() loop instead of doing time.Now for every current update
		m.battCurrent.time = m.time
		m.mtx.Unlock()
	}
}

func (m *Monitor) run() {
	t := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-t.C:
			m.mtx.Lock()
			m.time = time.Now()
			m.mtx.Unlock()
			log.Println("Current Charge:", m.currCharge)
			st, err := m.charger.sendCommand(query)
			if err != nil {
				log.Println("Error querying charger", err)
				continue
			}
			log.Println(st)
			m.mtx.Lock()
			shouldStop := st == charging && m.currCharge >= 780 && time.Since(m.battCurrent.time) < 1*time.Minute && m.battCurrent.current > 5
			m.mtx.Unlock()
			if shouldStop {
				log.Println("Reached charge limit, stopping charging")
				_, err := m.charger.sendCommand(sleep)
				if err != nil {
					log.Println("Error sleeping charger", err)
				}
			}
		}
	}
}
