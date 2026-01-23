package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"tinygo.org/x/bluetooth"
)

// --- Configuration ---
const Address = "C0:D6:3C:58:A4:10"

// --- UUIDs ---
var (
	ServiceUUID = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf0, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	NotifyUUID  = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf1, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	WriteUUID   = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xf2, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
	AuthUUID    = bluetooth.NewUUID([16]byte{0x00, 0x00, 0xff, 0xfa, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb})
)

// --- Commands ---
var (
	AuthPayload  = []byte("HiLink")
	CmdHeartbeat = []byte{0x7E, 0x00, 0x01, 0x03, 0x00, 0x92, 0x00, 0x00, 0x9F, 0x22, 0x0D}
	CmdGetData   = []byte{0x1E, 0x00, 0x01, 0x03, 0x00, 0x8C, 0x00, 0x00, 0xB1, 0x44, 0x0D}
)

var adapter = bluetooth.DefaultAdapter

func main() {
	// Enable BLE interface
	must("enable adapter", adapter.Enable())

	// 1. Scan and Connect
	fmt.Printf("Scanning for %s...\n", Address)
	var device bluetooth.Device
	found := false

	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		if result.Address.String() == Address {
			fmt.Println("Device found! Connecting...")
			var err error
			device, err = adapter.Connect(result.Address, bluetooth.ConnectionParams{})
			if err != nil {
				log.Printf("Failed to connect: %s", err)
				return
			}
			found = true
			adapter.StopScan()
		}
	})
	must("scan", err)

	if !found {
		log.Fatal("Device not found")
	}
	defer device.Disconnect()

	// 2. Discover Services
	fmt.Println("Discovering services...")
	srvs, err := device.DiscoverServices([]bluetooth.UUID{ServiceUUID})
	must("discover services", err)
	service := srvs[0]

	// 3. Get Characteristics
	chars, err := service.DiscoverCharacteristics([]bluetooth.UUID{NotifyUUID, WriteUUID, AuthUUID})
	must("discover characteristics", err)

	var notifyChar, writeChar, authChar bluetooth.DeviceCharacteristic
	for _, c := range chars {
		switch c.UUID() {
		case NotifyUUID:
			notifyChar = c
		case WriteUUID:
			writeChar = c
		case AuthUUID:
			authChar = c
		}
	}

	// 4. Subscribe to Notifications
	fmt.Println("Subscribing to data...")
	err = notifyChar.EnableNotifications(notificationHandler)
	must("enable notifications", err)

	// 5. Authenticate
	fmt.Println("Authenticating...")
	_, err = authChar.WriteWithoutResponse(AuthPayload)
	must("auth", err)
	time.Sleep(1 * time.Second)

	// 6. Start the Loop (Heartbeat & Data Request)
	fmt.Println("Starting Loop (Ctrl+C to stop)...")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// We need to alternate commands roughly like the Python script
	for range ticker.C {
		// Send Heartbeat (Keep-Alive)
		// fmt.Println(">> Sending Heartbeat")
		_, err := writeChar.WriteWithoutResponse(CmdHeartbeat)
		if err != nil {
			log.Printf("Error writing heartbeat: %v", err)
			break
		}

		time.Sleep(500 * time.Millisecond)

		// Send Data Request
		// fmt.Println(">> Sending Data Request")
		_, err = writeChar.WriteWithoutResponse(CmdGetData)
		if err != nil {
			log.Printf("Error writing data req: %v", err)
			break
		}
	}
}

func notificationHandler(buf []byte) {
	// Filter short packets and wrong registers
	if len(buf) < 10 {
		return
	}
	// Byte 5 is Register ID
	if buf[5] != 0x8C {
		return
	}

	// --- PARSING LOGIC ---
	// Data starts at index 8 based on your logs
	cursor := 8

	// 1. Cells
	numCells := int(buf[cursor])
	cursor++

	// Skip Cell Data (2 bytes * numCells)
	cursor += numCells * 2

	// 2. Temps
	numTemps := int(buf[cursor])
	cursor++

	// Skip Temp Data (2 bytes * numTemps)
	cursor += numTemps * 2

	// --- MAPPED DATA ---

	// Slot A: Current + Status Flags
	// We read Uint16 and perform bit masking
	slotA := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	cursor += 2

	// Decode Current/Status
	current, statusStr := decodeCurrentAndStatus(slotA)

	// Slot B: Voltage
	voltsRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	voltage := float64(voltsRaw) / 100.0
	cursor += 2

	// Slot C: Remaining Capacity
	remRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	remCap := float64(remRaw) / 10.0
	cursor += 2

	// Slot D: Full Capacity
	fullRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	fullCap := float64(fullRaw) / 10.0
	cursor += 2

	// Slot E: Cycles
	cycles := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	cursor += 2

	// Slot F: Design Capacity
	designRaw := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	designCap := float64(designRaw) / 10.0
	cursor += 2

	// Slot G: SOC
	soc := binary.BigEndian.Uint16(buf[cursor : cursor+2])
	// cursor += 2

	// --- PRINT OUTPUT ---
	fmt.Print("\033[H\033[J") // Clear screen
	fmt.Println("┌──────────────────────────────────────────┐")
	fmt.Printf("│  BATTERY STATUS (Go)        SOC: %3d%%   │\n", soc)
	fmt.Println("├──────────────────────────────────────────┤")
	fmt.Printf("│  Voltage:  %6.2f V                    │\n", voltage)
	fmt.Printf("│  Current:  %6.1f A   (%s)  │\n", current, padString(statusStr, 11))
	fmt.Printf("│  Capacity: %6.1f / %5.1f Ah         │\n", remCap, fullCap)
	fmt.Printf("│  Cycles:   %-6d (Design: %4.0fAh) │\n", cycles, designCap)
	fmt.Println("└──────────────────────────────────────────┘")
}

func decodeCurrentAndStatus(raw uint16) (float64, string) {
	// Top 2 bits are status
	// 0xC000 is the mask for top 2 bits (1100 0000 ...)
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

	// Bottom 14 bits are current value (0x3FFF mask)
	val := raw & 0x3FFF
	current := float64(val) / 10.0

	// Apply sign
	if statusStr == "Discharging" {
		current = -current
	}

	return current, statusStr
}

func must(action string, err error) {
	if err != nil {
		log.Fatalf("Failed to %s: %s", action, err)
	}
}

func padString(s string, length int) string {
	if len(s) >= length {
		return s
	}
	padding := make([]byte, length-len(s))
	for i := range padding {
		padding[i] = ' '
	}
	return s + string(padding)
}
