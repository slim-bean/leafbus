package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/slim-bean/leafbus/pkg/stream"
)

type metricServer struct {
	sc *synchronizer
}

func NewMetricServer(s *synchronizer) *metricServer {
	return &metricServer{
		sc: s,
	}
}

func (s *metricServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("Received metrics request")
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
	} else {
		rate = 10 * time.Millisecond
	}

	// Create Sync

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
	c := make(chan *stream.Data, 500)
	go s.metricLoader(c, start, end, name, rate)

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}

	var currEntry *stream.Data
	lastTimestamp := time.Unix(0, 0)

	for nextTimestamp := range syncChan {
		for {
			time.Sleep(1 * time.Millisecond)
			// Dequeue next entry
			if currEntry == nil {
				b, ok := <-c
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

func (s *metricServer) metricLoader(c chan *stream.Data, start, end time.Time, queryString string, rate time.Duration) {
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
		if adjustedEnd.After(end) {
			log.Println("End of Data")
			return
		}
		r := v1.Range{
			Start: start,
			End:   adjustedEnd,
			Step:  rate,
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
					if len(st.Values) <= 1 {
						start = start.Add(1 * time.Minute)
						break
					}
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

						c <- d
						lastSent = entry.Timestamp.Time()
						start = entry.Timestamp.Time()
					}
				}
			}
		}
		// Wait for buffer to be less than half full
		for len(c) > 250 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}
