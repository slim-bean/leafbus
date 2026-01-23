


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

ARM_CC ?= aarch64-linux-gnu-gcc

arm:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=$(ARM_CC) go build -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
send: arm
	scp cmd/leafbus/leafbus pi@leaf.edjusted.com:

can:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cantest/cantest ./cmd/cantest/main.go
send-can: can
	scp cmd/cantest/cantest pi@leaf.edjusted.com:

hydra:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/hydratest/hydratest ./cmd/hydratest/main.go
send-hydra: hydra
	scp cmd/hydratest/hydratest pi@leaf.edjusted.com:

ms4525:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/ms4525/ms4525 ./cmd/ms4525/main.go
send-ms4525: ms4525
	scp cmd/ms4525/ms4525 pi@leaf.edjusted.com:

gps:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/gps/gps ./cmd/gps/main.go
send-gps: gps
	scp cmd/gps/gps pi@leaf.edjusted.com:

cam:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cam/cam ./cmd/cam/main.go
send-cam: cam
	scp cmd/cam/cam pi@leaf.edjusted.com:

reader:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/reader/reader ./cmd/reader/main.go
send-reader: reader
	scp cmd/reader/reader pi@leaf.edjusted.com:

playback:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/playback/playback ./cmd/playback/main.go
send-playback: playback
	scp cmd/playback/playback pi@leaf.edjusted.com:

wattcycle:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o cmd/wattcycletest/wattcycletest ./cmd/wattcycletest/main.go
send-wattcycle: wattcycle
	scp cmd/wattcycletest/wattcycletest pi@leaf.edjusted.com: