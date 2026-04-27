FROM golang:1.22-bookworm AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN mkdir -p /out \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/watchdog ./cmd/watchdog \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/watchdog-supervisor ./cmd/watchdog-supervisor \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/watchdogctl ./cmd/watchdogctl \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/watchdog-sim-module ./cmd/watchdog-sim-module

FROM debian:bookworm-slim
WORKDIR /var/lib/watchdog
COPY --from=builder /out/watchdog /usr/local/bin/watchdog
COPY --from=builder /out/watchdog-supervisor /usr/local/bin/watchdog-supervisor
COPY --from=builder /out/watchdogctl /usr/local/bin/watchdogctl
COPY --from=builder /out/watchdog-sim-module /usr/local/bin/watchdog-sim-module
ENTRYPOINT []
