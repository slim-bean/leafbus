package stream

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

var (
	dataPool = sync.Pool{
		New: func() interface{} {
			return &Data{
				Name:      "",
				Timestamp: 0,
				Val:       0,
			}
		},
	}
)

func GetData() *Data {
	return dataPool.Get().(*Data)
}

type Data struct {
	Name      string
	Timestamp int64
	Val       float64
}

func (d *Data) String() string {
	return fmt.Sprintf("%v %v\n", d.Timestamp, d.Val)
}

type Streamer struct {
	send chan *Data
}

func NewStreamer() *Streamer {
	return &Streamer{send: make(chan *Data, 100)}
}

func (s *Streamer) SendData(d *Data) {
	//If we can't keep up, drop samples
	if len(s.send) >= 100 {
		dataPool.Put(d)
		log.Println("backed up sending data over http")
		return
	}
	s.send <- d
}

func setupResponse(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

func (s *Streamer) Handler(w http.ResponseWriter, r *http.Request) {
	setupResponse(&w, r)
	if (*r).Method == "OPTIONS" {
		return
	}
	//counter = counter + 1
	//id := counter

	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}

	query := r.URL.Query()
	if query.Get("TEST") != "" {
		fmt.Fprintln(w, "OK")
		return
	}

	name := query.Get("name")
	if name == "" {
		http.Error(w, "Missing name parameter", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	for {
		select {
		case d := <-s.send:
			if d.Name == name {
				fmt.Fprintf(w, "%v", d)
				flusher.Flush()
			}
			dataPool.Put(d)
		}

	}

}
