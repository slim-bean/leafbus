module github.com/slim-bean/leafbus

go 1.13

require (
	github.com/adrianmo/go-nmea v1.1.0
	github.com/brutella/can v0.0.1
	github.com/cortexproject/cortex v1.2.1-0.20200803161316-7014ff11ed70
	github.com/d2r2/go-i2c v0.0.0-20191123181816-73a8a799d6bc
	github.com/d2r2/go-logger v0.0.0-20181221090742-9998a510495e
	github.com/gdamore/tcell v1.3.0
	github.com/go-kit/kit v0.12.0
	github.com/grafana/loki-client-go v0.0.0-20210114103019-6057f29bbf5b
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.30.0
	github.com/prometheus/prometheus v1.8.2-0.20201028100903-3245b3267b24
	github.com/rivo/tview v0.0.0-20200127143856-e8d152077496
	github.com/weaveworks/common v0.0.0-20200625145055-4b1847531bc9
	go.bug.st/serial v1.0.0
	google.golang.org/grpc v1.40.0
)

//// Override reference that causes an error from Go proxy - see https://github.com/golang/go/issues/33558
//replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
//
//// Override reference causing proxy error.  Otherwise it attempts to download https://proxy.golang.org/golang.org/x/net/@v/v0.0.0-20190813000000-74dc4d7220e7.info
//replace golang.org/x/net => golang.org/x/net v0.0.0-20190923162816-aa69164e4478
