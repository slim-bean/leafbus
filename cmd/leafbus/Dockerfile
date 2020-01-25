FROM golang:1.13-alpine AS builder
COPY . /build
WORKDIR /build
RUN CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o leafbus ./cmd/leafbus/main.go
RUN mv /build/leafbus /

FROM alpine
WORKDIR /
#RUN apk add --no-cache file && \
#    rm -rf /var/cache/apk/*
COPY --from=builder /leafbus /leafbus
CMD ["/leafbus"]
