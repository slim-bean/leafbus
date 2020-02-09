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

	"github.com/slim-bean/leafbus/pkg/stream"
)

var (
	batchSize = 1000

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

type cortex struct {
	cortex    client.HealthAndIngesterClient
	data      chan *packet
	active    []*packet
	actMtx    sync.Mutex
	streamMap map[string][]*ratedFollower
	streamMtx sync.Mutex
}

func newCortex(address string) (*cortex, error) {
	fs := flag.NewFlagSet("", flag.PanicOnError)
	cfg := client.Config{
		GRPCClientConfig: grpcclient.Config{},
	}
	cfg.RegisterFlags(fs)
	clt, err := client.MakeIngesterClient(address, cfg)
	if err != nil {
		return nil, err
	}
	c := &cortex{
		cortex:    clt,
		data:      make(chan *packet, 100),
		active:    packetsPool.Get().([]*packet),
		streamMap: map[string][]*ratedFollower{},
	}
	go c.run()
	return c, nil
}

type ratedFollower struct {
	*stream.Follower
	lastSent int64
}

func (c *cortex) follow(name string, follower *stream.Follower) {
	c.streamMtx.Lock()
	defer c.streamMtx.Unlock()
	if _, ok := c.streamMap[name]; ok {
		for i := range c.streamMap[name] {
			if c.streamMap[name][i].Follower == follower {
				log.Println("ERROR, stream is already being followed with this channel")
				return
			}
		}
		log.Printf("New follower registered for: %v, count: %v\n", name, len(c.streamMap[name]))
		f := &ratedFollower{
			Follower: follower,
			lastSent: 0,
		}
		c.streamMap[name] = append(c.streamMap[name], f)
	} else {
		log.Println("First follower registered for: ", name)
		f := &ratedFollower{
			Follower: follower,
			lastSent: 0,
		}
		c.streamMap[name] = []*ratedFollower{f}
	}
}

func (c *cortex) unfollow(name string, follower *stream.Follower) {
	c.streamMtx.Lock()
	defer c.streamMtx.Unlock()
	if _, ok := c.streamMap[name]; !ok {
		log.Println("ERROR, tried to unfollow a stream not being followed")
		return
	} else {
		for i := range c.streamMap[name] {
			if c.streamMap[name][i].Follower == follower {
				c.streamMap[name][i] = c.streamMap[name][len(c.streamMap[name])-1]
				c.streamMap[name][len(c.streamMap[name])-1] = nil
				c.streamMap[name] = c.streamMap[name][:len(c.streamMap[name])-1]
				log.Printf("Removed follower for metric %v, %v remaining followers", name, len(c.streamMap[name]))
				if len(c.streamMap[name]) == 0 {
					log.Printf("No longer following any streams for metric %v, removing\n", name)
					delete(c.streamMap, name)
				}
				return
			}
		}
		log.Printf("ERROR: Failed to remove follower for %v, did not find any matching channels", name)
	}
}

func (c *cortex) run() {
	for {
		select {
		case p := <-c.data:

			//Send to any live streamers
			if _, ok := c.streamMap[p.labels.Get(name)]; ok {
				for _, f := range c.streamMap[p.labels.Get(name)] {
					if len(f.Pub) >= 1 {
						continue
					}
					// Check to see if it's time to send another sample based on the rate requested, if rate is enabled.
					if f.Rate > 0 {
						if p.sample.TimestampMs-f.lastSent < f.Rate {
							// Too soon, skip this sample
							continue
						} else {
							// Equal or exceeded rate, update last send and allow this sample to be sent.
							f.lastSent = p.sample.TimestampMs
						}
					}
					d := stream.GetData()
					d.Timestamp = p.sample.TimestampMs
					d.Val = p.sample.Value
					f.Pub <- d
				}
			}

			// Add to batch
			c.actMtx.Lock()
			c.active = append(c.active, p)

			// If batch is full, send to cortex
			if len(c.active) == batchSize {
				sending := c.active
				c.active = packetsPool.Get().([]*packet)
				go c.push(sending)
			}
			c.actMtx.Unlock()
		}
	}
}

func (c *cortex) push(ps []*packet) {
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
