package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/slim-bean/leafbus/pkg/hydra"
)

func main() {

	h, err := hydra.NewHydra(nil, "/dev/ttyUSB0")
	if err != nil {
		log.Fatal(err)
	}

	err = h.EnterBinaryMode()
	if err != nil {
		log.Fatal(err)
	}

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)

	go func() {
		select {
		case <-c:
			os.Exit(1)
		}
	}()

	select {}

}
