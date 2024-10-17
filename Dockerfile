# syntax=docker/dockerfile:1
ARG GO_VERSION=1.23.2
ARG ALPINE_VERSION=3.20
FROM golang:${GO_VERSION} AS builder

RUN apt-get update
RUN apt-get install -y \
    git \
    gcc \
    musl-dev \
    libvips-dev

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
COPY samples samples

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix -ldflags="-s -w" \
    -o ./rudder-load-producer \
    cmd/producer/main.go

FROM ubuntu:noble

RUN apt-get update
RUN apt-get install -y \
    tzdata ca-certificates curl

WORKDIR /

RUN mkdir samples
COPY --from=builder /app/samples samples
COPY --from=builder /app/rudder-load-producer .

CMD ["/rudder-load-producer"]
