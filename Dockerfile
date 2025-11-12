# syntax=docker/dockerfile:1
ARG GO_VERSION=1.24.6
ARG ALPINE_VERSION=3.21
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

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

FROM alpine:${ALPINE_VERSION}

# Install dependencies
RUN apk --no-cache upgrade && \
    apk --no-cache add tzdata curl bash zstd-libs

WORKDIR /

RUN mkdir templates
COPY --from=builder /app/templates templates
COPY --from=builder /app/rudder-load-producer .
COPY --from=builder /app/rudder-load-keydb .
