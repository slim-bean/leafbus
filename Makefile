


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

arm:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
send: arm
	scp cmd/leafbus/leafbus ubuntu@leaf.edjusted.com:

arm-test:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/cantest/cantest ./cmd/cantest/main.go
send-test: arm-test
	scp cmd/cantest/cantest ubuntu@leaf.edjusted.com:

hydra:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/hydratest/hydratest ./cmd/hydratest/main.go
send-hydra: hydra
	scp cmd/hydratest/hydratest ubuntu@leaf.edjusted.com:
