


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .

arm:
	env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o cmd/leafbus/leafbus ./cmd/leafbus/main.go
send: arm
	scp cmd/leafbus/leafbus ubuntu@leaf.edjusted.com:
