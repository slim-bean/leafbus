package charge

import (
	"log"
	"time"

	"github.com/brutella/can"
)

type Monitor struct {
	charger    *openevse
	currCharge uint16
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
	if frame.ID != 0x55B {
		return
	}
	m.currCharge = (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
}

func (m *Monitor) run() {
	t := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-t.C:
			log.Println("Current Charge:", m.currCharge)
			st, err := m.charger.getChargerStatus()
			if err != nil {
				log.Println("Error querying charger", err)
				continue
			}
			log.Println(st)
		}
	}
}
