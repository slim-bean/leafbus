package stream

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
)

var (
	dataPool = sync.Pool{
		New: func() interface{} {
			return &Data{
				Timestamp: 0,
				Val:       0,
				//bytes:     []byte{'s', 'n', 'p', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			}
		},
	}
)

type FollowStream interface {
	Follow(string, *Follower)
	Unfollow(string, *Follower)
}

type Follower struct {
	Pub  chan *Data
	Rate int64
}

func GetData() *Data {
	return dataPool.Get().(*Data)
}

type Data struct {
	Name      string  `json:"name"`
	Timestamp int64   `json:"ts"`
	Val       float64 `json:"val"`
	//bytes     []byte
}

func (d *Data) String() string {
	return fmt.Sprintf("%v %v %v\n", d.Name, d.Timestamp, d.Val)
}

//func (d *Data) Bytes() []byte {
//	//binary.BigEndian.PutUint64(d.bytes[3:11], math.Float64bits(float64(d.Timestamp)))
//	//binary.BigEndian.PutUint64(d.bytes[11:], math.Float64bits(d.Val))
//	b, err := json.Marshal(d)
//	if err != nil {
//		log.Println("Failed to marshal livestream data:", err)
//		return []byte{}
//	}
//	return b
//}

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

	var rate int64
	rateQuery := query.Get("rate")
	if rateQuery != "" && rateQuery != "undefined" {
		rt, err := strconv.ParseInt(rateQuery, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to parse rate as int64: %v", err), http.StatusBadRequest)
			return
		}
		rate = rt
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	f := &Follower{
		Pub:  make(chan *Data, 1),
		Rate: rate,
	}
	s.handler.Follow(name, f)
	defer func() {
		log.Println("Unfollowing")
		s.handler.Unfollow(name, f)
		close(f.Pub)
	}()
	for {
		select {
		case d := <-f.Pub:
			err := enc.Encode(d)
			if err != nil {
				log.Println("Failed to marshal data object to json stream:", err)
			}
			flusher.Flush()
			reuseData(d)
		case <-r.Context().Done():
			log.Println("Client connection closed")
			return
		}
	}
}

func reuseData(data *Data) {
	dataPool.Put(data)
}
