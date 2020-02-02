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

type FollowStream interface {
	Follow(string, chan *Data)
	Unfollow(string, chan *Data)
}

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
	handler FollowStream
}

func NewStreamer(handler FollowStream) *Streamer {
	return &Streamer{handler: handler}
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
	channel := make(chan *Data, 1)
	s.handler.Follow(name, channel)
	defer func() {
		log.Println("Unfollowing")
		s.handler.Unfollow(name, channel)
		close(channel)
	}()
	for {
		select {
		case d := <-channel:
			if d.Name == name {
				fmt.Fprintf(w, "%v", d)
				flusher.Flush()
			}
			dataPool.Put(d)
		case <-r.Context().Done():
			log.Println("Client connection closed")
			return
		}
	}
}
