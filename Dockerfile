FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /watchfetcher ./cmd/fetcher \
 && CGO_ENABLED=0 go build -o /watchengine ./cmd/engine \
 && CGO_ENABLED=0 go build -o /watchingest ./cmd/ingest \
 && CGO_ENABLED=0 go build -o /watchapi ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates bash util-linux
WORKDIR /app
COPY --from=builder /watchfetcher /watchengine /watchingest /watchapi /usr/local/bin/
COPY config ./config
COPY scripts ./scripts
# data volume mount at /data; also support /app/data for local
RUN mkdir -p /data /app/data && chmod +x /app/scripts/run-nightly.sh /app/scripts/start.sh
ENV DB_PATH=/data/pricing.sqlite
ENV PORT=8080
CMD ["bash", "/app/scripts/start.sh"]
