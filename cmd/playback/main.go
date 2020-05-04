package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/slim-bean/leafbus/pkg/stream"
)

func main() {
	controlChannel := make(chan int)
	sc := newSynchroinzer(controlChannel)

	imageServer := &imageServer{
		sc,
		make(chan *loghttp.Entry, 20),
	}

	metricServer := &metricServer{
		sc,
		make(chan *stream.Data, 500),
	}

	log.Println("Starting web server on 9999")
	http.HandleFunc("/mjpeg", imageServer.ServeHTTP)
	http.HandleFunc("/metrics", metricServer.ServeHTTP)
	http.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		run := r.Form.Get("run")
		start, end, err := bounds(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		if strings.ToLower(run) == "true" {
			log.Println("Starting Services from HTTP Request")
			sc.run(start, end, 5)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	})

	if err := http.ListenAndServe(":9999", nil); err != nil {
		log.Println(err)
	}

}

type synchronizer struct {
	controlChanel chan int
	syncChannels  []chan *time.Time
	syncMtx       sync.Mutex
}

func newSynchroinzer(controlChanel chan int) *synchronizer {
	return &synchronizer{
		controlChanel: controlChanel,
		syncChannels:  []chan *time.Time{},
		syncMtx:       sync.Mutex{},
	}
}

func (s *synchronizer) addSyncChannel() chan *time.Time {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	c := make(chan *time.Time, 10)
	s.syncChannels = append(s.syncChannels, c)
	return c
}

func (s *synchronizer) removeSyncChannel(c chan *time.Time) {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	idx := -1
	for i := range s.syncChannels {
		if s.syncChannels[i] == c {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	s.syncChannels[idx] = s.syncChannels[len(s.syncChannels)-1]
	s.syncChannels[len(s.syncChannels)-1] = nil
	s.syncChannels = s.syncChannels[:len(s.syncChannels)-1]
}

func (s *synchronizer) run(start, end time.Time, scale int64) {
	next := start
	lastSent := time.Unix(0, 0)
	ticker := time.NewTicker(1 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			time.Sleep(1 * time.Millisecond)
			now := time.Now()
			elapsed := now.Sub(lastSent).Milliseconds()
			// Default 10ms tick for timestamps
			if elapsed < 10/scale {
				continue
			}
			lastSent = now
			// Send time to channels
			s.syncMtx.Lock()
			for _, c := range s.syncChannels {
				if len(c) < 10 {
					c <- &next
				} else {
					log.Println("A sender is not keeping up, TS dropped")
				}

			}
			s.syncMtx.Unlock()
			// Add the fixed 10ms to the counter
			next = next.Add(10 * time.Millisecond)
			if next.After(end) {
				log.Println("Reached end timestamp")
				break
			}
		}
	}
}

type imageServer struct {
	sc *synchronizer
	c  chan *loghttp.Entry
}

func (s *imageServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var rate time.Duration
	rateQuery := r.Form.Get("rate")
	if rateQuery != "" && rateQuery != "undefined" {
		rt, err := strconv.ParseInt(rateQuery, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to parse rate as int64: %v", err), http.StatusBadRequest)
			return
		}
		rate = time.Duration(rt) * time.Millisecond
	}

	start, end, err := bounds(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	syncChan := s.sc.addSyncChannel()
	defer func() {
		s.sc.removeSyncChannel(syncChan)
	}()
	go s.imageLoader(start, end)

	m := multipart.NewWriter(w)
	defer m.Close()

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+m.Boundary())
	w.Header().Set("Connection", "close")
	h := textproto.MIMEHeader{}
	st := fmt.Sprint(time.Now().Unix())
	var currEntry *loghttp.Entry
	lastTimestamp := time.Unix(0, 0)

	for {
		select {
		case nextTimestamp := <-syncChan:
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

				// Throw this point away because the requested rate is less than we are receiving images
				if currentTimestamp.Before(lastTimestamp.Add(rate)) {
					currEntry = nil
					break
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
				currEntry = nil
				break
			}
		}
	}
}

func (s *imageServer) imageLoader(start, end time.Time) {
	lastSent := time.Unix(0, 0)

	for {

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
		log.Println("# Streams:", len(streams))
		for i, stream := range streams {
			for j, entry := range stream.Entries {
				if !entry.Timestamp.After(lastSent) {
					log.Println("ignoring old entry")
					continue
				}
				log.Println("Pushing image to queue with time:", entry.Timestamp)

				s.c <- &streams[i].Entries[j]
				lastSent = entry.Timestamp
				start = entry.Timestamp
			}
		}
		for len(s.c) > 10 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

type metricServer struct {
	sc *synchronizer
	c  chan *stream.Data
}

func (s *metricServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var rate time.Duration
	rateQuery := r.Form.Get("rate")
	if rateQuery != "" && rateQuery != "undefined" {
		rt, err := strconv.ParseInt(rateQuery, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to parse rate as int64: %v", err), http.StatusBadRequest)
			return
		}
		rate = time.Duration(rt) * time.Millisecond
	}

	start, end, err := bounds(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}

	syncChan := s.sc.addSyncChannel()
	defer func() {
		s.sc.removeSyncChannel(syncChan)
	}()
	go s.metricLoader(start, end, name)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}

	var currEntry *stream.Data
	lastTimestamp := time.Unix(0, 0)

	for {
		select {
		case nextTimestamp := <-syncChan:
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
				currentTimestamp := time.Unix(0, currEntry.Timestamp*int64(1e6))

				// Need to wait for next timestamp to catch up, keep point but continue
				if currentTimestamp.After(*nextTimestamp) {
					break
				}

				// Throw this point away because the requested rate is less than we are receiving images
				if currentTimestamp.Before(lastTimestamp.Add(rate)) {
					currEntry = nil
					break
				}

				//log.Println("Sending Metric:", name, "Value:", currentTimestamp)
				err := enc.Encode(currEntry)
				if err != nil {
					log.Println("Failed to marshal data object to json stream:", err)
				}
				flusher.Flush()

				lastTimestamp = currentTimestamp
				currEntry = nil
				break
			}
		}
	}
}

func (s *metricServer) metricLoader(start, end time.Time, queryString string) {
	lastSent := time.Unix(0, 0)
	client, err := api.NewClient(api.Config{
		Address: "http://localhost:8002/api/prom",
	})
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return
	}
	v1api := v1.NewAPI(client)
	for {
		// ?query=battery_amps&start=1588515010&end=1588516734&step=2
		adjustedEnd := start.Add(1 * time.Minute)
		r := v1.Range{
			Start: start,
			End:   adjustedEnd,
			Step:  10 * time.Millisecond,
		}
		log.Println("Querying:", queryString, "Range:", r)
		result, warnings, err := v1api.QueryRange(context.Background(), queryString, r)
		if err != nil {
			log.Printf("Error querying Prometheus: %v\n", err)
			return
		}
		if len(warnings) > 0 {
			log.Printf("Warnings: %v\n", warnings)
		}

		if m, ok := result.(model.Matrix); ok {
			for i, st := range m {
				if st.Metric.String() == queryString {
					for j, entry := range st.Values {
						if !entry.Timestamp.After(model.TimeFromUnixNano(lastSent.UnixNano())) {
							log.Println("ignoring old entry")
							continue
						}
						//log.Println("Pushing image to queue with time:", entry.Timestamp)
						d := &stream.Data{
							Name:      queryString,
							Timestamp: m[i].Values[j].Timestamp.UnixNano() / 1e6,
							Val:       float64(m[i].Values[j].Value),
						}

						s.c <- d
						lastSent = entry.Timestamp.Time()
						start = entry.Timestamp.Time()
					}
				}
			}
		}
		// Wait for buffer to be less than half full
		for len(s.c) > 250 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func bounds(r *http.Request) (time.Time, time.Time, error) {
	start, err := parseTimestamp(r.Form.Get("start"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseTimestamp(r.Form.Get("end"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

// parseUnixNano parses a ns unix timestamp from a string
// if the value is empty it returns a default value passed as second parameter
func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("missing start or end")
	}

	if strings.Contains(value, ".") {
		if t, err := strconv.ParseFloat(value, 64); err == nil {
			s, ns := math.Modf(t)
			ns = math.Round(ns*1000) / 1000
			return time.Unix(int64(s), int64(ns*float64(time.Second))), nil
		}
	}
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return ts, nil
		}
		return time.Time{}, err
	}
	if len(value) <= 10 {
		return time.Unix(nanos, 0), nil
	}
	return time.Unix(0, nanos), nil
}
