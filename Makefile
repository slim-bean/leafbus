


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

arm:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
	upx cmd/leafbus/leafbus
send: arm
	scp cmd/leafbus/leafbus pi@leaf.edjusted.com:

docker-arm-64:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

arm-64:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
	upx cmd/leafbus/leafbus
send-64: arm-64
	scp cmd/leafbus/leafbus pi@leaf.edjusted.com:


can:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cantest/cantest ./cmd/cantest/main.go
	upx cmd/cantest/cantest
send-can: can
	scp cmd/cantest/cantest pi@leaf.edjusted.com:

hydra:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/hydratest/hydratest ./cmd/hydratest/main.go
	upx cmd/hydra/hydra
send-hydra: hydra
	scp cmd/hydratest/hydratest pi@leaf.edjusted.com:

ms4525:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/ms4525/ms4525 ./cmd/ms4525/main.go
	upx cmd/ms4525/ms4525
send-ms4525: ms4525
	scp cmd/ms4525/ms4525 pi@leaf.edjusted.com:

gps:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/gps/gps ./cmd/gps/main.go
	upx cmd/gps/gps
send-gps: gps
	scp cmd/gps/gps pi@leaf.edjusted.com:

cam:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cam/cam ./cmd/cam/main.go
	upx cmd/cam/cam
send-cam: cam
	scp cmd/cam/cam pi@leaf.edjusted.com:

reader:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/reader/reader ./cmd/reader/main.go
	upx cmd/reader/reader
send-reader: reader
	scp cmd/reader/reader pi@leaf.edjusted.com:

playback:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/playback/playback ./cmd/playback/main.go
	upx cmd/playback/playback
send-playback: playback
	scp cmd/playback/playback pi@leaf.edjusted.com:
