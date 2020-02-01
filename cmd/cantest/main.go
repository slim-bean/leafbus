package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"unsafe"

	"github.com/brutella/can"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

const (
	zero = "0"
	one  = "1"
)

func main() {

	log.Println("Finding interface")
	iface, err := net.InterfaceByName("can0")

	if err != nil {
		log.Fatalf("Could not find network interface %s (%v)", "can0", err)
	}
	log.Println("Opening interface")
	conn, err := can.NewReadWriteCloserForInterface(iface)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Building UI")
	table := tview.NewTable().SetBorders(false)
	table.SetCell(0, 1, tview.NewTableCell("7").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 2, tview.NewTableCell("6").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 3, tview.NewTableCell("5").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 4, tview.NewTableCell("4").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 5, tview.NewTableCell("3").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 6, tview.NewTableCell("2").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 7, tview.NewTableCell("1").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(0, 8, tview.NewTableCell("0").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(1, 0, tview.NewTableCell("0").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(2, 0, tview.NewTableCell("1").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(3, 0, tview.NewTableCell("2").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(4, 0, tview.NewTableCell("3").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(5, 0, tview.NewTableCell("4").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(6, 0, tview.NewTableCell("5").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(7, 0, tview.NewTableCell("6").SetTextColor(tcell.ColorGreenYellow))
	table.SetCell(8, 0, tview.NewTableCell("7").SetTextColor(tcell.ColorGreenYellow))
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			table.SetCell(i+1, j+1, tview.NewTableCell("-"))
		}
	}

	var frameID uint32
	frameID = 0x1DB

	text := tview.NewTextView()

	flex := tview.NewFlex().
		AddItem(tview.NewFrame(table).AddText(fmt.Sprintf("Packet %X", frameID), true, tview.AlignCenter, tcell.ColorWhite), 20, 1, false).
		AddItem(tview.NewFrame(text).AddText("Calcs", true, tview.AlignCenter, tcell.ColorWhite), 20, 1, false)

	app := tview.NewApplication()

	log.Println("Creating handler")
	h := NewHandler(app, table, text, frameID)

	log.Println("Creating new Bus and subscribing")
	bus := can.NewBus(conn)
	bus.SubscribeFunc(h.Handle)

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)

	go func() {
		select {
		case <-c:
			bus.Disconnect()
			os.Exit(1)
		}
	}()

	log.Println("Starting publish loop")
	go func() {
		err = bus.ConnectAndPublish()
		if err != nil {
			log.Println(err)
		}
	}()

	log.Println("Launching UI")
	if err := app.SetRoot(flex, true).Run(); err != nil {
		panic(err)
	}

	log.Println("Exiting")
}

type Handler struct {
	app   *tview.Application
	table *tview.Table
	text  *tview.TextView
	id    uint32
}

func NewHandler(app *tview.Application, table *tview.Table, text *tview.TextView, id uint32) *Handler {
	return &Handler{
		app:   app,
		table: table,
		text:  text,
		id:    id,
	}
}

func (h *Handler) Handle(frame can.Frame) {
	// Only care about current charge status

	if h.id == frame.ID {
		h.app.QueueUpdateDraw(func() {
			for i, v := range frame.Data {
				for j := 0; j < 8; j++ {
					c := h.table.GetCell(i+1, (7-j)+1)
					bit := (v & (1 << j)) >> j
					if bit == 0 {
						c.SetText(zero)
					} else if bit == 1 {
						c.SetText(one)
					} else {
						c.SetText("x")
					}
					h.table.SetCell(i+1, (7-j)+1, c)
				}
			}

			//Calcs

			// Even though the doc says the LSB for current is 0.5 it seems to reflect the actual charger current
			// more accurately when I don't ignore the last bit
			var battCurrent int16
			if frame.Data[0]&0b10000000 == 0b10000000 {
				battCurrent = int16((uint16(frame.Data[0]) << 3) | 0b1111100000000000 | uint16(frame.Data[1]>>6))
			} else {
				battCurrent = int16((uint16(frame.Data[0])<<3)&0b0000011111111111 | uint16(frame.Data[1]>>6))
			}
			h.text.SetText(fmt.Sprintf("Battery Amps: %v", battCurrent))
			//var motorAmps int16
			//if frame.Data[2]&0b00000100 == 0b00000100 {
			//	motorAmps = int16(((uint16(frame.Data[2]&0b00000111) << 8) | 0b1111100000000000) | uint16(frame.Data[3]))
			//} else {
			//	motorAmps = int16(((uint16(frame.Data[2]&0b00000111) << 8) & 0b0000011111111111) | uint16(frame.Data[3]))
			//}
			//motorSpeed := int16(uint16(frame.Data[4])<<8 | uint16(frame.Data[5]))
			//h.text.SetText(fmt.Sprintf("Motor Amps: %v\nMotor Speed: %v\nA: %v", motorAmps, motorSpeed, strconv.FormatInt(int64(motorAmps), 2)))
		})
	}
	//switch frame.ID {
	//case 0x55B:
	//
	//	//currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)
	//
	//case 0x1DA:
	//	//var motorAmps, motorSpd int16
	//	//if frame.Data[2] & 0b00001000 == 0b00001000 {
	//	//	motorAmps = int16((uint16(frame.Data[2]&0b00001111) << 8)|0b1111000000000000 | (uint16(frame.Data[3])))
	//	//} else {
	//	//	motorAmps = int16((uint16(frame.Data[2]&0b00001111) << 8)&0b0000111111111111 | (uint16(frame.Data[3])))
	//	//}
	//	//
	//	////
	//	//motorSpd = int16((uint16(frame.Data[4]) << 8) | (uint16(frame.Data[5])))
	//	////
	//	//fmt.Println("Amps", motorAmps, "Speed", motorSpd)
	//
	//case 0x1DB:
	//
	//	var battCurrent int16
	//	if frame.Data[0]&0b10000000 == 0b10000000 {
	//		battCurrent = int16((uint16(frame.Data[0]) << 3) | 0b1111100000000000 | uint16(frame.Data[1]>>6))
	//	} else {
	//		battCurrent = int16((uint16(frame.Data[0])<<3)&0b0000011111111111 | uint16(frame.Data[1]>>6))
	//	}
	//	currVoltage := (uint16(frame.Data[2]) << 1) | (uint16(frame.Data[3]&0b11000000) >> 7)
	//	//
	//	fmt.Println("0", strconv.FormatUint(uint64(frame.Data[0]), 2), "1", strconv.FormatUint(uint64(frame.Data[1]), 2), "BattAmps", float64(battCurrent), "0", strconv.FormatUint(uint64(frame.Data[2]), 2), "1", strconv.FormatUint(uint64(frame.Data[3]), 2), "BattVolts", float64(currVoltage))
	//
	//	//fmt.Println("0", strconv.FormatUint(uint64(frame.Data[0]), 2),"1", strconv.FormatUint(uint64(frame.Data[1]), 2))
	//
	//	//fmt.Println("0", frame.Data[0], "1", frame.Data[1])
	//	//v := (uint16(frame.Data[0]) << 3) | (uint16(frame.Data[1]&0b11100000) >> 5)
	//	////signed := *(*int16)(unsafe.Pointer(&v))
	//	//fmt.Println(strconv.FormatInt(int64(int16convert(v)), 2), int16convert(v))
	//
	//}
}

func int16convert(f uint16) int16 {
	return *(*int16)(unsafe.Pointer(&f))
}
