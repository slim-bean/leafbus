package gps

import (
	"fmt"
	"log"
	"time"

	"github.com/adrianmo/go-nmea"
	"github.com/prometheus/prometheus/pkg/labels"
	"go.bug.st/serial"

	"github.com/slim-bean/leafbus/pkg/push"
)

var (
	gpsLabels = labels.Labels{
		labels.Label{
			Name:  "job",
			Value: "gps",
		},
	}
)

type GPS struct {
	handler   *push.Handler
	p         serial.Port
	sendChan  chan string
	runChan   chan bool
	shouldRun bool
}

func NewGPS(handler *push.Handler, port string) (*GPS, error) {
	mode := &serial.Mode{}
	p, err := serial.Open(port, mode)
	if err != nil {
		return nil, err
	}
	gps := &GPS{
		handler:   handler,
		p:         p,
		sendChan:  make(chan string),
		runChan:   make(chan bool),
		shouldRun: false,
	}

	go gps.run()
	go gps.read()

	return gps, nil
}

func (g *GPS) Start() {
	g.runChan <- true
}

func (g *GPS) Stop() {
	g.runChan <- false
}

func (n *GPS) read() {
	// Read and print the response
	buff := make([]byte, 100)
	sent := make([]byte, 500)

	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case r := <-n.runChan:
			n.shouldRun = r
		case <-ticker.C:
			if !n.shouldRun {
				continue
			}
			// Reads up to 100 bytes
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
					//fmt.Println(st)
					n.sendChan <- st
					sent = sent[:0]
				}
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
				if m.Validity == "A" {
					n.handler.SendLog(gpsLabels, time.Date(m.Date.YY+2000, time.Month(m.Date.MM), m.Date.DD, m.Time.Hour, m.Time.Minute, m.Time.Second, m.Time.Millisecond*1000000, time.UTC), fmt.Sprintf("%s,%s", nmea.FormatGPS(m.Latitude), nmea.FormatGPS(m.Longitude)))
				} else {
					log.Println("Invalid GPS Signal")
				}
				//n.handler.SendLog(gpsLabels, time.Now(), fmt.Sprintf("%s,%s", nmea.FormatGPS(m.Latitude), nmea.FormatGPS(m.Longitude)))
				//fmt.Println(time.Date(m.Date.YY+2000, time.Month(m.Date.MM), m.Date.DD, m.Time.Hour, m.Time.Minute, m.Time.Second, m.Time.Millisecond*1000000, time.UTC))
				//fmt.Printf("Raw sentence: %v\n", m)
				//fmt.Printf("Time: %s\n", m.Time)
				//fmt.Printf("Validity: %s\n", m.Validity)
				//fmt.Printf("Latitude GPS: %s\n", nmea.FormatGPS(m.Latitude))
				//fmt.Printf("Latitude DMS: %s\n", nmea.FormatDMS(m.Latitude))
				//fmt.Printf("Longitude GPS: %s\n", nmea.FormatGPS(m.Longitude))
				//fmt.Printf("Longitude DMS: %s\n", nmea.FormatDMS(m.Longitude))
				//fmt.Printf("Speed: %f\n", m.Speed)
				//fmt.Printf("Course: %f\n", m.Course)
				//fmt.Printf("Date: %s\n", m.Date)
				//fmt.Printf("Variation: %f\n", m.Variation)
			}
		}

	}

}
