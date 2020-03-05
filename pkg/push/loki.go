package push

import (
	"context"
	"flag"
	"log"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	lokiclient "github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"
)

var (
	lokiBatchSize = 10
)

type loki struct {
	loki       grpc_health_v1.HealthClient
	data       chan *singleLog
	ctx        context.Context
	active     map[model.Fingerprint]*logproto.Stream
	batchCount int
	actMtx     sync.Mutex
}

func newLoki(address string) (*loki, error) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	cfg := lokiclient.Config{
		GRPCClientConfig: grpcclient.Config{},
	}
	cfg.RegisterFlags(fs)
	clt, err := lokiclient.New(cfg, address)
	if err != nil {
		return nil, err
	}
	l := &loki{
		loki:   clt,
		data:   make(chan *singleLog),
		ctx:    context.Background(),
		active: make(map[model.Fingerprint]*logproto.Stream),
	}
	go l.run()
	return l, nil
}

type singleLog struct {
	Labels labels.Labels
	Entry  *logproto.Entry
}

func (l *loki) run() {
	for {
		select {
		case p := <-l.data:

			fp := client.FastFingerprint(client.FromLabelsToLabelAdapters(p.Labels))

			// Add to batch
			l.actMtx.Lock()

			if _, ok := l.active[fp]; ok {
				l.active[fp].Entries = append(l.active[fp].Entries, *p.Entry)
			} else {
				//entries := make([]logproto.Entry, lokiBatchSize)
				//entries = append(entries, *p.Entry)
				l.active[fp] = &logproto.Stream{Labels: p.Labels.String(), Entries: []logproto.Entry{*p.Entry}}
			}
			l.batchCount++

			// If batch is full, send to cortex
			if l.batchCount == lokiBatchSize {
				streams := make([]*logproto.Stream, len(l.active))
				i := 0
				for fp := range l.active {
					streams[i] = l.active[fp]
					i++
				}
				//log.Println("Map:", l.active)
				//log.Println("Streams:", streams)
				l.batchCount = 0
				for fp := range l.active {
					delete(l.active, fp)
				}

				go l.push(streams)
			}
			l.actMtx.Unlock()
		}
	}
}

func (l *loki) push(streams []*logproto.Stream) {
	req := &logproto.PushRequest{
		Streams: streams,
	}
	//for i := range req.Streams {
	//	log.Println("Labels:", req.Streams[i].Labels)
	//	for j := range req.Streams[i].Entries {
	//		log.Println(req.Streams[i].Entries[j])
	//	}
	//	log.Println()
	//}

	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	defer cancel()
	ctx = user.InjectOrgID(ctx, "leaf")
	ctx = user.InjectUserID(ctx, "fake")
	_, err := l.loki.(logproto.PusherClient).Push(ctx, req)
	if err != nil {
		log.Println("Failed to send logs to Loki:", err)
		return
	}
	log.Println("Batch sent to Loki")
}
