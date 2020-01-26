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
	labelsPool = sync.Pool{
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
	active *TimeSeries
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
		active: timeSeriesPool.Get().(*TimeSeries),
	}
	go c.run()
	return c, nil
}

func (c *Cortex) run() {
	for {
		select {
		case p := <-c.data:
			c.actMtx.Lock()
			c.active.Labels = append(c.active.Labels, p.labels)
			c.active.Samples = append(c.active.Samples, p.sample)
			p.labels = p.labels[:0]
			packetPool.Put(p)
			if len(c.active.Labels) == batchSize {
				wr := client.ToWriteRequest(c.active.Labels, c.active.Samples, client.API)
				sending := c.active
				c.active = timeSeriesPool.Get().(*TimeSeries)
				go c.push(sending, wr)
			}
			c.actMtx.Unlock()
		}
	}
}

func (c *Cortex) push(ts *TimeSeries, wr *client.WriteRequest) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	//_, err := c.cortex.Push(ctx, wr)
	//if err != nil {
	//	log.Println("Failed to end series to ingester: ", err)
	//}
	log.Printf("Sending batch to cortex: %v\n", wr.Timeseries)
	client.ReuseSlice(wr.Timeseries)
	reuseTimeseries(ts)
}

// ReuseTimeseries puts the timeseries back into a sync.Pool for reuse.
func reuseTimeseries(ts *TimeSeries) {
	for i := range ts.Labels {
		for j := range ts.Labels[i] {
			labelsPool.Put(ts.Labels[i][j])
		}
		ts.Labels[i] = ts.Labels[i][:0]
	}
	ts.Labels = ts.Labels[:0]
	for i := range ts.Samples {
		samplePool.Put(ts.Samples[i])
	}
	ts.Samples = ts.Samples[:0]
	timeSeriesPool.Put(ts)
}
