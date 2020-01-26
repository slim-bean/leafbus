package push

import (
	"context"
	"flag"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
)

type Cortex struct {
	cortex client.HealthAndIngesterClient
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
	c := &Cortex{cortex: clt}
	return c, nil
}

func (c *Cortex) Push(timeseries []client.PreallocTimeseries) error {
	req := client.WriteRequest{
		Timeseries: timeseries,
	}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	_, err := c.cortex.Push(ctx, &req)
	if err != nil {
		return err
	}
	return nil
}

// Need to receive labels and values into some pre-allocated batch size array which we can send to cortex
