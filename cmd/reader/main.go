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
		make(chan *loghttp.Entry, 20),
	}

	log.Println("Starting web server on 9999")
	http.HandleFunc("/mjpeg", server.ServeHTTP)

	go func() {
		if err := http.ListenAndServe(":9999", nil); err != nil {
			log.Println(err)
		}
	}()

	//log.Println("Sleeping for 15s")
	//time.Sleep(15 * time.Second)
	//log.Println("Done Sleeping, querying")

	lastSent := time.Unix(0, 0)
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatal("Failed to parse timezonez:", err)
	}
	//start := time.Date(2020, 03, 05, 0, 5, 0, 0, loc)
	//end := time.Date(2020, 03, 05, 0, 30, 0, 0, loc)
	start := time.Date(2020, 03, 05, 17, 0, 0, 0, loc)
	end := time.Date(2020, 03, 05, 17, 38, 0, 0, loc)

	for {
		//end := time.Now()
		//start := time.Now().Add(-30 * time.Second)

		u := url.URL{
			Scheme: "http",
			Host:   "localhost:8003",
			Path:   "loki/api/v1/query_range",
			RawQuery: fmt.Sprintf("start=%d&end=%d&direction=FORWARD", start.UnixNano(), end.UnixNano()) +
				"&query=" + url.QueryEscape(fmt.Sprintf("{job=\"camera\"}")) +
				"&limit=20",
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
		for i, stream := range streams {
			for j, entry := range stream.Entries {
				if !entry.Timestamp.After(lastSent) {
					log.Println("ignoring old entry")
					continue
				}
				log.Println("Pushing image to queue with time:", entry.Timestamp)

				server.c <- &streams[i].Entries[j]
				lastSent = entry.Timestamp
				start = entry.Timestamp
				//time.Sleep(1 * time.Second)
				//err = ioutil.WriteFile("reader.jpg", bytes, 0644)
				//if err != nil {
				//	log.Println("Error writing file:", err)
				//}
			}
		}
		for len(server.c) > 10 {
			time.Sleep(10 * time.Millisecond)
		}
	}

}

type server struct {
	c chan *loghttp.Entry
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	m := multipart.NewWriter(w)
	defer m.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
	w.Header().Set("Connection", "close")
	h := textproto.MIMEHeader{}
	st := fmt.Sprint(time.Now().Unix())
	var currEntry *loghttp.Entry
	lastSent := time.Unix(0, 0)
	lastTimestamp := time.Unix(0, 0)
	scale := int64(5)
	for {
		time.Sleep(10 * time.Millisecond)
		if currEntry == nil {
			b, ok := <-s.c
			if !ok {
				break
			}
			currEntry = b
			//log.Println("Dequeued", b.Timestamp, "Curr", currEntry.Timestamp)
		}
		elapsed := time.Now().Sub(lastSent).Milliseconds()
		relative := currEntry.Timestamp.Sub(lastTimestamp).Milliseconds() / scale
		//log.Println("Elapsed", elapsed, "Relative", relative)
		if elapsed < relative {
			continue
		}
		log.Println("Sending Image: ", currEntry.Timestamp)
		bytes, err := base64.StdEncoding.DecodeString(currEntry.Line)
		if err != nil {
			log.Println("Error base64 decoding:", err)
			return
		}
		h.Set("Content-Type", "image/jpeg")
		h.Set("Content-Length", fmt.Sprint(len(bytes)))
		h.Set("X-StartTime", st)
		h.Set("X-TimeStamp", fmt.Sprint(time.Now().Unix()))
		mw, err := m.CreatePart(h)
		if err != nil {
			break
		}
		_, err = mw.Write(bytes)
		if err != nil {
			break
		}
		if flusher, ok := mw.(http.Flusher); ok {
			flusher.Flush()
		}

		lastTimestamp = currEntry.Timestamp
		lastSent = time.Now()
		currEntry = nil

	}
}
