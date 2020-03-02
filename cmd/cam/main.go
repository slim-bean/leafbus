package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/slim-bean/leafbus/pkg/cam"
	"github.com/slim-bean/leafbus/pkg/push"
)

func main() {
	log.Println("Creating handler")
	handler, err := push.NewHandler("localhost:9002", "localhost:9003")
	if err != nil {
		log.Fatal(err)
	}
	_, _ = cam.NewCam(handler)
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
