# WatchLedger production image: single static binary, embedded templates/static.
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /watchledger-web ./cmd/web \
 && CGO_ENABLED=0 go build -o /watchledger-engine ./cmd/engine \
 && CGO_ENABLED=0 go build -o /watchledger-ingest ./cmd/ingest \
 && CGO_ENABLED=0 go build -o /watchledger-reproduce ./cmd/reproduce \
 && CGO_ENABLED=0 go build -o /watchledger-alerts ./cmd/alerts

FROM alpine:3.20
RUN apk add --no-cache ca-certificates bash
WORKDIR /app
COPY --from=builder /watchledger-web /watchledger-engine /watchledger-ingest /watchledger-reproduce /watchledger-alerts /usr/local/bin/
COPY scripts/prod-start.sh /app/scripts/prod-start.sh
RUN chmod +x /app/scripts/prod-start.sh
RUN mkdir -p /data
ENV DB_PATH=/data/watchledger.sqlite
ENV PORT=8080
EXPOSE 8080
CMD ["bash", "/app/scripts/prod-start.sh"]
