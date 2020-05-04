package main

import (
	"log"
	"net/http"

	"github.com/slim-bean/leafbus/pkg/playback"
)

func main() {
	controlChannel := make(chan int)
	synchroinzer := playback.NewSynchroinzer(controlChannel)

	imageServer := playback.NewImageServer(synchroinzer)
	metricServer := playback.NewMetricServer(synchroinzer)

	log.Println("Starting web server on 9999")
	http.HandleFunc("/mjpeg", imageServer.ServeHTTP)
	http.HandleFunc("/metrics", metricServer.ServeHTTP)
	http.HandleFunc("/control", synchroinzer.ServeHTTP)

	if err := http.ListenAndServe(":9999", nil); err != nil {
		log.Println(err)
	}

}
