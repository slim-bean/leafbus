package charge

import (
	"log"
	"time"

	"github.com/brutella/can"

	"github.com/slim-bean/leafbus/pkg/push"
)

type Monitor struct {
	charger    *openevse
	currCharge uint16
	handler    *push.Handler
}

func NewMonitor(chargerAddress string, handler *push.Handler) (*Monitor, error) {
	ch, err := newopenevse(chargerAddress)
	if err != nil {
		return nil, err
	}
	m := &Monitor{
		charger: ch,
		handler: handler,
	}
	go m.run()
	return m, nil
}

func (m *Monitor) SetHandler(handler *push.Handler) {
	m.handler = handler
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
			st, err := m.charger.sendCommand(query)
			if err != nil {
				log.Println("Error querying charger", err)
				continue
			}
			log.Println(st)
			if m.handler != nil {
				m.handler.UpdateCharger(time.Now(), st.String(), float64(m.currCharge)/10)
			}
			if st == charging && m.currCharge >= 780 {
				log.Println("Reached charge limit, stopping charging")
				_, err := m.charger.sendCommand(sleep)
				if err != nil {
					log.Println("Error sleeping charger", err)
				}
			}
		}
	}
}
