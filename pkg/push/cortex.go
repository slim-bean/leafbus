package push

import (
	"context"
	"flag"
	"log"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"
)

var (
	batchSize = 100

	timeSeriesPool = sync.Pool{
		New: func() interface{} {
			return &TimeSeries{
				Labels:  make([]labels.Labels, 0, batchSize),
				Samples: make([]client.Sample, 0, batchSize),
			}
		},
	}
	packetsPool = sync.Pool{
		New: func() interface{} {
			return make([]*packet, 0, batchSize)
		},
	}
	packetPool = sync.Pool{
		New: func() interface{} {
			return &packet{
				sample: samplePool.Get().(client.Sample),
				labels: make([]labels.Label, 0, 10),
			}
		},
	}
	samplePool = sync.Pool{
		New: func() interface{} {
			return client.Sample{
				Value:       0,
				TimestampMs: 0,
			}
		},
	}
	labelPool = sync.Pool{
		New: func() interface{} {
			return labels.Label{}
		},
	}
)

type TimeSeries struct {
	Labels  []labels.Labels
	Samples []client.Sample
}

type packet struct {
	labels labels.Labels
	sample client.Sample
}

type Cortex struct {
	cortex client.HealthAndIngesterClient
	data   chan *packet
	active []*packet
	actMtx sync.Mutex
}

func NewCortex(address string) (*Cortex, error) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	cfg := client.Config{
		GRPCClientConfig: grpcclient.Config{},
	}
	cfg.RegisterFlags(fs)
	clt, err := client.MakeIngesterClient(address, cfg)
	if err != nil {
		return nil, err
	}
	c := &Cortex{
		cortex: clt,
		data:   make(chan *packet, 100),
		active: packetsPool.Get().([]*packet),
	}
	go c.run()
	return c, nil
}

func (c *Cortex) run() {
	for {
		select {
		case p := <-c.data:
			c.actMtx.Lock()
			c.active = append(c.active, p)
			if len(c.active) == batchSize {
				sending := c.active
				c.active = packetsPool.Get().([]*packet)
				go c.push(sending)
			}
			c.actMtx.Unlock()
		}
	}
}

func (c *Cortex) push(ps []*packet) {
	ts := timeSeriesPool.Get().(*TimeSeries)
	for i := range ps {
		ts.Labels = append(ts.Labels, ps[i].labels)
		ts.Samples = append(ts.Samples, ps[i].sample)
	}
	wr := client.ToWriteRequest(ts.Labels, ts.Samples, client.API)
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	log.Printf("Sending batch to cortex\n")
	ctx = user.InjectOrgID(ctx, "leaf")
	ctx = user.InjectUserID(ctx, "fake")
	_, err := c.cortex.Push(ctx, wr)
	if err != nil {
		log.Println("Failed to end series to ingester: ", err)
		log.Printf("Timeseries: %v", ts)
		log.Printf("Samples: %v", wr.Timeseries)
	}
	client.ReuseSlice(wr.Timeseries)
	reuseTimeseries(ts)
	reusePackets(ps)
}

// ReuseTimeseries puts the timeseries back into a sync.Pool for reuse.
func reuseTimeseries(ts *TimeSeries) {
	ts.Labels = ts.Labels[:0]
	ts.Samples = ts.Samples[:0]
	timeSeriesPool.Put(ts)
}

func reusePackets(ps []*packet) {
	// For each packet
	for i := range ps {
		// For each label in the packets label slice
		for j := range ps[i].labels {
			// Return the label
			labelPool.Put(ps[i].labels[j])
		}
		// Clear out the label slice
		ps[i].labels = ps[i].labels[:0]
		// Return the sample
		samplePool.Put(ps[i].sample)
		// Return the packet
		packetPool.Put(ps[i])
	}
	// Clear out the packet slice
	ps = ps[:0]
	// Return the packet slice
	packetsPool.Put(ps)
}
