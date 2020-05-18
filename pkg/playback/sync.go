package playback

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type synchronizer struct {
	controlChanel chan int
	syncChannels  map[int64][]chan *time.Time
	syncMtx       sync.Mutex
	activeSync    int64
	done          chan struct{}
}

func NewSynchroinzer(controlChanel chan int) *synchronizer {
	return &synchronizer{
		controlChanel: controlChanel,
		syncChannels:  map[int64][]chan *time.Time{},
		syncMtx:       sync.Mutex{},
		done:          make(chan struct{}),
	}
}

func (s *synchronizer) addSyncChannel(id int64) chan *time.Time {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	c := make(chan *time.Time, 10)
	if _, ok := s.syncChannels[id]; ok {
		s.syncChannels[id] = append(s.syncChannels[id], c)
	} else {
		s.syncChannels[id] = []chan *time.Time{c}
	}
	return c
}

func (s *synchronizer) removeSyncChannel(id int64, c chan *time.Time) {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	if e, ok := s.syncChannels[id]; ok {
		idx := -1
		for i := range e {
			if e[i] == c {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		e[idx] = e[len(e)-1]
		e[len(e)-1] = nil
		e = e[:len(e)-1]
	}
}

func (s *synchronizer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	run := r.Form.Get("run")
	start, end, err := bounds(r)
	//log.Println("Sync Start:", start, " End:", end)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	if strings.ToLower(run) == "start" {
		log.Println("Starting Services from HTTP Request")
		go s.run(start, end, 2)
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.ToLower(run) == "reset" {
		log.Println("Resetting")
		s.resetAll()
		s.done <- struct{}{}
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
}

func (s *synchronizer) reset(start, end time.Time) {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	id := start.UnixNano() ^ end.UnixNano()
	if _, ok := s.syncChannels[id]; ok {
		for _, c := range s.syncChannels[id] {
			close(c)
		}
		s.syncChannels[id] = s.syncChannels[id][:0]
		delete(s.syncChannels, id)
	}
}

func (s *synchronizer) resetAll() {
	s.syncMtx.Lock()
	defer s.syncMtx.Unlock()
	toDelete := []int64{}
	for key := range s.syncChannels {
		if _, ok := s.syncChannels[key]; ok {
			for _, c := range s.syncChannels[key] {
				close(c)
			}
			s.syncChannels[key] = s.syncChannels[key][:0]
			toDelete = append(toDelete, key)
		}
	}
	for _, key := range toDelete {
		delete(s.syncChannels, key)
	}

}

func (s *synchronizer) run(start, end time.Time, scale int64) {
	next := start
	lastSent := time.Unix(0, 0)
	ticker := time.NewTicker(1 * time.Millisecond)
	for {
		select {
		case <-s.done:
			log.Println("Sync runner requested to shutdown, shutting down")
			return
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(lastSent).Milliseconds()
			// Default 10ms tick for timestamps
			if elapsed < 10/scale {
				continue
			}
			lastSent = now
			// Send time to channels
			s.syncMtx.Lock()
			for _, cs := range s.syncChannels {
				for _, c := range cs {
					if len(c) < 10 {
						c <- &next
					} else {
						//log.Println("A sender is not keeping up, TS dropped")
					}
				}
			}
			s.syncMtx.Unlock()
			// Add the fixed 10ms to the counter
			next = next.Add(10 * time.Millisecond)
			if next.After(end) {
				log.Println("Reached end timestamp")
				return
			}
		}
	}
}
