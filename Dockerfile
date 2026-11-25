# syntax=docker/dockerfile:1
FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

# Install dependencies
RUN apk --no-cache add --update make tzdata ca-certificates gcc musl-dev zstd-dev

# Create app directory
RUN mkdir /app
WORKDIR /app

# Copy Go modules files and download dependencies
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy selectively for better security
COPY cmd cmd
COPY internal internal
COPY templates templates

# Build binaries
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix -ldflags="-s -w" \
    -o ./rudder-load-producer \
    cmd/producer/*.go

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix -ldflags="-s -w" \
    -o ./rudder-load-keydb \
    cmd/keydb/*.go

# alpine3.24.2
FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# Install dependencies
RUN apk --no-cache upgrade && \
    apk --no-cache add tzdata curl bash zstd-libs

WORKDIR /

RUN mkdir templates
COPY --from=builder /app/templates templates
COPY --from=builder /app/rudder-load-producer .
COPY --from=builder /app/rudder-load-keydb .
