package wattcycle

import (
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	DefaultAddress = "C0:D6:3C:58:A4:10"
)

var (
	serviceUUID = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf0, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	notifyUUID  = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf1, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	writeUUID   = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf2, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	authUUID    = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xfa, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
)

var (
	authPayload  = []byte("HiLink")
	cmdHeartbeat = []byte{0x7E, 0x00, 0x01, 0x03, 0x00, 0x92, 0x00, 0x00, 0x9F, 0x22, 0x0D}
	cmdGetData   = []byte{0x1E, 0x00, 0x01, 0x03, 0x00, 0x8C, 0x00, 0x00, 0xB1, 0x44, 0x0D}
)

type Config struct {
	Address      string
	PollInterval time.Duration
}

type Status struct {
	Timestamp   time.Time
	SOC         float64
	Voltage     float64
	Current     float64
	Status      string
	RemainingAh float64
	FullAh      float64
	Cycles      uint16
	DesignAh    float64
	TempsC      []float64
}

type Monitor struct {
	cfg      Config
	adapter  *bluetooth.Adapter
	statusCh chan Status
	stopCh   chan struct{}
	stopped  chan struct{}
	mu       sync.Mutex
	running  bool
	lastMu   sync.Mutex
	last     Status
}

func NewMonitor(cfg Config) (*Monitor, error) {
	if cfg.Address == "" {
		cfg.Address = DefaultAddress
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &Monitor{
		cfg:      cfg,
		adapter:  bluetooth.DefaultAdapter,
		statusCh: make(chan Status, 50),
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
	}, nil
}

func (m *Monitor) Statuses() <-chan Status {
	return m.statusCh
}

func (m *Monitor) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.mu.Unlock()
	go m.run()
	return nil
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()
	close(m.stopCh)
	<-m.stopped
	close(m.statusCh)
}

func (m *Monitor) run() {
	defer close(m.stopped)
	if err := m.adapter.Enable(); err != nil {
		log.Println("failed to enable bluetooth adapter:", err)
		return
	}
	device, err := m.scanAndConnect()
	if err != nil {
		log.Println("failed to connect to wattcycle:", err)
		return
	}
	defer device.Disconnect()

	srvs, err := device.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil || len(srvs) == 0 {
		log.Println("failed to discover wattcycle services:", err)
		return
	}
	service := srvs[0]
	chars, err := service.DiscoverCharacteristics([]bluetooth.UUID{notifyUUID, writeUUID, authUUID})
	if err != nil {
		log.Println("failed to discover wattcycle characteristics:", err)
		return
	}

	var notifyChar, writeChar, authChar bluetooth.DeviceCharacteristic
	for _, c := range chars {
		switch c.UUID() {
		case notifyUUID:
			notifyChar = c
		case writeUUID:
			writeChar = c
		case authUUID:
			authChar = c
		}
	}

	if err := notifyChar.EnableNotifications(m.notificationHandler); err != nil {
		log.Println("failed to enable wattcycle notifications:", err)
		return
	}
	if _, err := authChar.WriteWithoutResponse(authPayload); err != nil {
		log.Println("failed to authenticate wattcycle:", err)
		return
	}
	time.Sleep(1 * time.Second)

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := writeChar.WriteWithoutResponse(cmdHeartbeat); err != nil {
				log.Println("error writing heartbeat:", err)
				return
			}
			time.Sleep(500 * time.Millisecond)
			if _, err := writeChar.WriteWithoutResponse(cmdGetData); err != nil {
				log.Println("error writing data request:", err)
				return
			}
		case <-logTicker.C:
			if st, ok := m.latestStatus(); ok {
				log.Printf("WattCycle 12V: %.2fV (SOC %.0f%%)\n", st.Voltage, st.SOC)
			}
		case <-m.stopCh:
			return
		}
	}
}

func (m *Monitor) scanAndConnect() (bluetooth.Device, error) {
	var device bluetooth.Device
	found := make(chan struct{})
	var once sync.Once
	err := m.adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		if result.Address.String() == m.cfg.Address {
			var err error
			device, err = adapter.Connect(result.Address, bluetooth.ConnectionParams{})
			if err != nil {
				log.Println("failed to connect:", err)
				return
			}
			adapter.StopScan()
			once.Do(func() { close(found) })
		}
	})
	if err != nil {
		return bluetooth.Device{}, err
	}
	select {
	case <-found:
		return device, nil
	case <-time.After(20 * time.Second):
		_ = m.adapter.StopScan()
		return bluetooth.Device{}, fmt.Errorf("timeout scanning for wattcycle device")
	}
}

func (m *Monitor) notificationHandler(buf []byte) {
	if len(buf) < 10 {
		return
	}
	if buf[5] != 0x8C {
		return
	}
	cursor := 8
	numCells := int(buf[cursor])
	cursor++
	cursor += numCells * 2

	numTemps := int(buf[cursor])
	cursor++
	temps := make([]float64, 0, numTemps)
	for i := 0; i < numTemps; i++ {
		raw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
		cursor += 2
		temps = append(temps, decodeTemp(raw))
	}

	slotA := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	cursor += 2
	current, statusStr := decodeCurrentAndStatus(slotA)

	voltsRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	voltage := float64(voltsRaw) / 100.0
	cursor += 2

	remRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	remCap := float64(remRaw) / 10.0
	cursor += 2

	fullRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	fullCap := float64(fullRaw) / 10.0
	cursor += 2

	cycles := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	cursor += 2

	designRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	designCap := float64(designRaw) / 10.0
	cursor += 2

	soc := binary.BigEndian.Uint16(buf[cursor : cursor+2])

	st := Status{
		Timestamp:   time.Now().UTC(),
		SOC:         float64(soc),
		Voltage:     voltage,
		Current:     current,
		Status:      statusStr,
		RemainingAh: remCap,
		FullAh:      fullCap,
		Cycles:      cycles,
		DesignAh:    designCap,
		TempsC:      temps,
	}

	select {
	case m.statusCh <- st:
	default:
		log.Println("wattcycle status buffer full, dropping")
	}
	m.setLatestStatus(st)
}

func decodeCurrentAndStatus(raw uint16) (float64, string) {
	flags := (raw >> 14) & 0x03
	statusStr := "Idle"
	switch flags {
	case 3:
		statusStr = "Discharging"
	case 2:
		statusStr = "Charging"
	case 1:
		statusStr = "Protect"
	}

	val := raw & 0x3FFF
	current := float64(val) / 10.0
	if statusStr == "Discharging" {
		current = -current
	}
	return current, statusStr
}

func decodeTemp(raw uint16) float64 {
	// Common BMS reports temperatures in 0.1K with a 273.1K offset.
	return (float64(raw) - 2731.0) / 10.0
}

func (m *Monitor) setLatestStatus(st Status) {
	m.lastMu.Lock()
	m.last = st
	m.lastMu.Unlock()
}

func (m *Monitor) latestStatus() (Status, bool) {
	m.lastMu.Lock()
	defer m.lastMu.Unlock()
	if m.last.Timestamp.IsZero() {
		return Status{}, false
	}
	return m.last, true
}
