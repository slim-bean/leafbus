package main

import (
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/slim-bean/leafbus/pkg/cam"
	"github.com/slim-bean/leafbus/pkg/push"
	"github.com/slim-bean/leafbus/pkg/store"
)

func main() {
	parquetDir := flag.String("parquet-dir", "", "Base directory for parquet output (required)")
	duckdbPath := flag.String("duckdb-path", "", "Optional path to the duckdb database file")
	flag.Parse()

	if *parquetDir == "" {
		log.Fatal("parquet-dir is required")
	}
	writer, err := store.NewWriter(*parquetDir, *duckdbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer writer.Close()

	log.Println("Creating handler")
	handler, err := push.NewHandler(writer)
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
