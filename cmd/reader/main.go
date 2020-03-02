package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
)

func main() {

	server := &server{
		make(chan []byte),
	}

	log.Println("Starting web server on 9999")
	http.HandleFunc("/mjpeg", server.ServeHTTP)

	go func() {
		if err := http.ListenAndServe(":9999", nil); err != nil {
			log.Println(err)
		}
	}()

	log.Println("Sleeping for 15s")
	time.Sleep(15 * time.Second)
	log.Println("Done Sleeping, querying")

	end := time.Now()
	start := time.Now().Add(-5 * time.Minute)

	u := url.URL{
		Scheme: "http",
		Host:   "localhost:8003",
		Path:   "loki/api/v1/query_range",
		RawQuery: fmt.Sprintf("start=%d&end=%d&direction=BACKWARD", start.UnixNano(), end.UnixNano()) +
			"&query=" + url.QueryEscape(fmt.Sprintf("{job=\"camera\"}")) +
			"&limit=30",
	}
	fmt.Println("Query:", u.String())

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		log.Println("Error building request:", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Error making request:", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()

	if resp.StatusCode/100 != 2 {
		buf, _ := ioutil.ReadAll(resp.Body)
		log.Printf("error response from server: %s (%v)", string(buf), err)
		return
	}
	var decoded loghttp.QueryResponse
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	if err != nil {
		log.Println("Error decoding json:", err)
		return
	}
	streams := decoded.Data.Result.(loghttp.Streams)
	log.Println("# Streams:", len(streams))
	for _, stream := range streams {
		for _, entry := range stream.Entries {
			log.Println("Writing image")
			bytes, err := base64.StdEncoding.DecodeString(entry.Line)
			if err != nil {
				log.Println("Error base64 decoding:", err)
				return
			}
			server.c <- bytes
			time.Sleep(1 * time.Second)
			//err = ioutil.WriteFile("reader.jpg", bytes, 0644)
			//if err != nil {
			//	log.Println("Error writing file:", err)
			//}
		}
	}
}

type server struct {
	c chan []byte
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	m := multipart.NewWriter(w)
	defer m.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
	w.Header().Set("Connection", "close")
	h := textproto.MIMEHeader{}
	st := fmt.Sprint(time.Now().Unix())
	for {
		b, ok := <-s.c
		if !ok {
			break
		}
		h.Set("Content-Type", "image/jpeg")
		h.Set("Content-Length", fmt.Sprint(len(b)))
		h.Set("X-StartTime", st)
		h.Set("X-TimeStamp", fmt.Sprint(time.Now().Unix()))
		mw, err := m.CreatePart(h)
		if err != nil {
			break
		}
		_, err = mw.Write(b)
		if err != nil {
			break
		}
		if flusher, ok := mw.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}
