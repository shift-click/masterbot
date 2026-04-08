FROM golang:1.25.5-alpine AS builder

ARG GOARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} go build -o /out/jucobot ./cmd/jucobot && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} go build -o /out/alertd ./cmd/alertd

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/jucobot /app/jucobot
COPY --from=builder /out/alertd /app/alertd
COPY configs/config.yaml /app/configs/config.yaml

RUN mkdir -p /app/data /tmp

ENTRYPOINT ["/app/jucobot", "--config", "/app/configs/config.yaml"]
