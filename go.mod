module github.com/slim-bean/leafbus

go 1.13

require (
	github.com/brutella/can v0.0.1
	github.com/cortexproject/cortex v0.4.0
	github.com/prometheus/prometheus v1.8.2-0.20190918104050-8744afdd1ea0
	github.com/rivo/tview v0.0.0-20200127143856-e8d152077496 // indirect
	github.com/weaveworks/common v0.0.0-20190822150010-afb9996716e4
	go.bug.st/serial v1.0.0
)

// Override reference that causes an error from Go proxy - see https://github.com/golang/go/issues/33558
replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
