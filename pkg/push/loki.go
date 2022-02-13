package push

import (
	"flag"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/grafana/loki-client-go/loki"
	"os"
)

type lokiclient struct {
	client *loki.Client
}

func newLoki(address string) (*lokiclient, error) {
	cfg := loki.Config{}
	// Sets defaults as well as anything from the command line
	cfg.RegisterFlags(flag.CommandLine)
	flag.Parse()

	var logger log.Logger
	logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "ts", log.DefaultTimestamp, "caller", log.DefaultCaller)
	logger = level.NewFilter(logger, level.AllowDebug())

	c, err := loki.NewWithLogger(cfg, logger)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create client", "err", err)
	}

	if err != nil {
		return nil, err
	}
	l := &lokiclient{
		client: c,
	}
	return l, nil
}
