module github.com/slim-bean/leafbus

go 1.13

require (
	github.com/brutella/can v0.0.1
	github.com/cortexproject/cortex v0.4.0
	github.com/d2r2/go-i2c v0.0.0-20191123181816-73a8a799d6bc
	github.com/d2r2/go-logger v0.0.0-20181221090742-9998a510495e
	github.com/prometheus/client_golang v1.4.1 // indirect
	github.com/prometheus/prometheus v1.8.2-0.20190918104050-8744afdd1ea0
	github.com/rivo/tview v0.0.0-20200127143856-e8d152077496 // indirect
	github.com/weaveworks/common v0.0.0-20190822150010-afb9996716e4
	go.bug.st/serial v1.0.0
)

// Override reference that causes an error from Go proxy - see https://github.com/golang/go/issues/33558
replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab

//replace github.com/d2r2/go-i2c => github.com/slim-bean/go-i2c v0.0.0-20200209222325-6203c45cebbb
