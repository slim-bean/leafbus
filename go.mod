module github.com/slim-bean/leafbus

go 1.13

require (
	contrib.go.opencensus.io/exporter/stackdriver v0.6.0 // indirect
	git.apache.org/thrift.git v0.0.0-20180924222215-a9235805469b // indirect
	github.com/Azure/azure-sdk-for-go v26.3.0+incompatible // indirect
	github.com/Azure/go-autorest v11.5.1+incompatible // indirect
	github.com/adrianmo/go-nmea v1.1.0
	github.com/brutella/can v0.0.1
	github.com/cortexproject/cortex v0.4.1-0.20191217132644-cd4009e2f8e7
	github.com/d2r2/go-i2c v0.0.0-20191123181816-73a8a799d6bc
	github.com/d2r2/go-logger v0.0.0-20181221090742-9998a510495e
	github.com/gdamore/tcell v1.3.0
	github.com/golang/lint v0.0.0-20180702182130-06c8688daad7 // indirect
	github.com/grafana/loki v1.3.0
	github.com/mattes/migrate v1.3.1 // indirect
	github.com/prometheus/client_golang v1.6.0
	github.com/prometheus/common v0.9.1
	github.com/prometheus/prometheus v1.8.2-0.20190918104050-8744afdd1ea0
	github.com/rivo/tview v0.0.0-20200127143856-e8d152077496
	github.com/weaveworks/common v0.0.0-20191103151037-0e7cefadc44f
	go.bug.st/serial v1.0.0
	google.golang.org/grpc v1.25.1
)

// Override reference that causes an error from Go proxy - see https://github.com/golang/go/issues/33558
replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab

// Override reference causing proxy error.  Otherwise it attempts to download https://proxy.golang.org/golang.org/x/net/@v/v0.0.0-20190813000000-74dc4d7220e7.info
replace golang.org/x/net => golang.org/x/net v0.0.0-20190923162816-aa69164e4478
