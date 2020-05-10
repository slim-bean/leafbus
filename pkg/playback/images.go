package playback

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

type imageServer struct {
	sc *synchronizer
	c  chan *loghttp.Entry
}

func NewImageServer(s *synchronizer) *imageServer {
	return &imageServer{
		sc: s,
		c:  make(chan *loghttp.Entry, 30),
	}
}

func setupResponse(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func (s *imageServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("New Image Request")
	setupResponse(&w, r)
	if (*r).Method == "OPTIONS" {
		return
	}
	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	name := r.Form.Get("name")
	if name == "" {
		http.Error(w, "Missing name parameter", http.StatusBadRequest)
		return
	}

	start, end, err := bounds(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	syncChan := s.sc.addSyncChannel(start.UnixNano() ^ end.UnixNano())
	defer func() {
		s.sc.removeSyncChannel(start.UnixNano()^end.UnixNano(), syncChan)
	}()
	done := make(chan struct{})
	defer func() {
		done <- struct{}{}
		log.Println("Exiting HTTP Image Request")
	}()
	go s.imageLoader(done, start, end)

	m := multipart.NewWriter(w)
	defer m.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
	w.Header().Set("Connection", "close")
	h := textproto.MIMEHeader{}
	st := fmt.Sprint(time.Now().Unix())
	var currEntry *loghttp.Entry
	//lastTimestamp := time.Unix(0, 0)

	for nextTimestamp := range syncChan {
		for {
			time.Sleep(1 * time.Millisecond)
			// Dequeue next entry
			if currEntry == nil {
				b, ok := <-s.c
				if !ok {
					log.Println("Sender out of data, channel closed")
					break
				}
				currEntry = b
			}
			currentTimestamp := currEntry.Timestamp

			// Need to wait for next timestamp to catch up, keep point but continue
			if currentTimestamp.After(*nextTimestamp) {
				break
			}

			//// Throw this point away because the requested rate is less than we are receiving images
			//if currentTimestamp.Before(lastTimestamp.Add(rate)) {
			//	currEntry = nil
			//	break
			//}

			//log.Println("Sending Image: ", currEntry.Timestamp)
			bytes, err := base64.StdEncoding.DecodeString(currEntry.Line)
			if err != nil {
				log.Println("Error base64 decoding:", err)
				return
			}
			h.Set("Content-Type", "image/jpeg")
			h.Set("Content-Length", fmt.Sprint(len(bytes)))
			if st == "" {
				st = fmt.Sprint(currentTimestamp.Unix())
			}
			h.Set("X-StartTime", st)
			h.Set("X-TimeStamp", fmt.Sprint(time.Now().Unix()))
			mw, err := m.CreatePart(h)
			if err != nil {
				break
			}
			_, err = mw.Write(bytes)
			if err != nil {
				return
			}
			if flusher, ok := mw.(http.Flusher); ok {
				flusher.Flush()
			}

			//lastTimestamp = currEntry.Timestamp
			currEntry = nil
			break
		}
	}

}

func (s *imageServer) imageLoader(done chan struct{}, start, end time.Time) {
	lastSent := time.Unix(0, 0)
	ticker := time.NewTicker(10 * time.Millisecond)
	for {
		select {
		case <-done:
			log.Println("Shutting down image loader thread")
			return
		case <-ticker.C:
			if len(s.c) > 10 {
				continue
			}
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
				log.Printf("error response from imageServer: %s (%v)", string(buf), err)
				return
			}
			var decoded loghttp.QueryResponse
			err = json.NewDecoder(resp.Body).Decode(&decoded)
			if err != nil {
				log.Println("Error decoding json:", err)
				return
			}
			streams := decoded.Data.Result.(loghttp.Streams)
			//log.Println("# Streams:", len(streams))
			for i, stream := range streams {
				for j, entry := range stream.Entries {
					if !entry.Timestamp.After(lastSent) {
						log.Println("ignoring old entry")
						continue
					}
					//log.Println("Pushing image to queue with time:", entry.Timestamp)

					s.c <- &streams[i].Entries[j]
					lastSent = entry.Timestamp
					start = entry.Timestamp
				}
			}

		}
	}
}
