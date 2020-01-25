


docker-arm:
	docker buildx build --platform linux/arm64 -f cmd/leafbus/Dockerfile -t slimbean/leafbus:latest --push .
