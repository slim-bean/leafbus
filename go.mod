module github.com/slim-bean/leafbus

go 1.13

require (
	github.com/adrianmo/go-nmea v1.1.0
	github.com/brutella/can v0.0.1
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/d2r2/go-i2c v0.0.0-20191123181816-73a8a799d6bc
	github.com/d2r2/go-logger v0.0.0-20181221090742-9998a510495e
	github.com/gdamore/tcell v1.3.0
	github.com/go-kit/kit v0.10.0
	github.com/go-kit/log v0.2.0
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v0.0.3 // indirect
	github.com/google/go-cmp v0.5.6 // indirect
	github.com/grafana/loki-client-go v0.0.0-20210114103019-6057f29bbf5b
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mitchellh/mapstructure v1.4.2 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/common v0.15.0
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/rivo/tview v0.0.0-20200127143856-e8d152077496
	go.bug.st/serial v1.0.0
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/goleak v1.1.11-0.20210813005559-691160354723 // indirect
	golang.org/x/net v0.0.0-20210917221730-978cfadd31cf // indirect
	golang.org/x/sys v0.0.0-20210917161153-d61c044b1678 // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/genproto v0.0.0-20210917145530-b395a37504d4 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)

//// Override reference that causes an error from Go proxy - see https://github.com/golang/go/issues/33558
//replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
//
//// Override reference causing proxy error.  Otherwise it attempts to download https://proxy.golang.org/golang.org/x/net/@v/v0.0.0-20190813000000-74dc4d7220e7.info
//replace golang.org/x/net => golang.org/x/net v0.0.0-20190923162816-aa69164e4478
