# syntax=docker/dockerfile:1
FROM golang:1.27.3-alpine3.24@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

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
FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# Install dependencies
RUN apk --no-cache upgrade && \
    apk --no-cache add tzdata curl bash zstd-libs

WORKDIR /

RUN mkdir templates
COPY --from=builder /app/templates templates
COPY --from=builder /app/rudder-load-producer .
COPY --from=builder /app/rudder-load-keydb .
