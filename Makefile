


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

arm:
	env GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
send: arm
	scp cmd/leafbus/leafbus pi@leaf.edjusted.com:

arm-test:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cantest/cantest ./cmd/cantest/main.go
send-test: arm-test
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
