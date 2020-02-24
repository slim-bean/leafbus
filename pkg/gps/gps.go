package gps

import (
	"fmt"
	"log"
	"time"

	"github.com/adrianmo/go-nmea"
	"go.bug.st/serial"

	"github.com/slim-bean/leafbus/pkg/push"
)

type GPS struct {
	handler  *push.Handler
	p        serial.Port
	sendChan chan string
}

func NewGPS(handler *push.Handler, port string) (*GPS, error) {
	mode := &serial.Mode{}
	p, err := serial.Open(port, mode)
	if err != nil {
		return nil, err
	}
	gps := &GPS{
		handler:  handler,
		p:        p,
		sendChan: make(chan string),
	}

	go gps.run()
	go gps.read()

	return gps, nil
}

func (n *GPS) read() {
	// Read and print the response
	buff := make([]byte, 100)
	sent := make([]byte, 500)
	for {
		// Reads up to 100 bytes
		time.Sleep(100 * time.Millisecond)
		nb, err := n.p.Read(buff)
		if err != nil {
			log.Println("Error reading from serial port:", err)
			continue
		}
		if nb == 0 {
			continue
		}
		for i := 0; i < nb; i++ {
			sent = append(sent, buff[i])
			if buff[i] == '\n' {
				st := string(sent[:len(sent)-2])
				fmt.Println(st)
				n.sendChan <- st
				sent = sent[:0]
			}
		}
	}
}

func (n *GPS) run() {

	for {
		select {
		case sent := <-n.sendChan:
			s, err := nmea.Parse(sent)
			if err != nil {
				log.Println("Error parsing sentence:", err)
				continue
			}

			switch m := s.(type) {
			case nmea.GPRMC:
				fmt.Printf("Raw sentence: %v\n", m)
				fmt.Printf("Time: %s\n", m.Time)
				fmt.Printf("Validity: %s\n", m.Validity)
				fmt.Printf("Latitude GPS: %s\n", nmea.FormatGPS(m.Latitude))
				fmt.Printf("Latitude DMS: %s\n", nmea.FormatDMS(m.Latitude))
				fmt.Printf("Longitude GPS: %s\n", nmea.FormatGPS(m.Longitude))
				fmt.Printf("Longitude DMS: %s\n", nmea.FormatDMS(m.Longitude))
				fmt.Printf("Speed: %f\n", m.Speed)
				fmt.Printf("Course: %f\n", m.Course)
				fmt.Printf("Date: %s\n", m.Date)
				fmt.Printf("Variation: %f\n", m.Variation)
			}
		}

	}

}
