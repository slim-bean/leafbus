package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/slim-bean/leafbus/pkg/gps"
)

func main() {

	g, err := gps.NewGPS(nil, "/dev/ttyAMA0")
	if err != nil {
		log.Fatal(err)
	}

	g.Start()
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)

	go func() {
		select {
		case <-c:
			g.Stop()
			os.Exit(0)
		}
	}()

	select {}

}
