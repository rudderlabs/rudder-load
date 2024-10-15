# syntax=docker/dockerfile:1
ARG GO_VERSION=1.23.2
ARG ALPINE_VERSION=3.20
FROM golang:${GO_VERSION} AS builder

WORKDIR /rudder-ingester

RUN apt-get update
RUN apt-get install -y \
    git \
    gcc \
    musl-dev \
    libvips-dev \
    libzstd-dev

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY cmd cmd
COPY internal internal
COPY samples samples

RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix -ldflags="-s -w"

FROM ubuntu:noble

RUN apt-get update
RUN apt-get install -y \
    tzdata ca-certificates curl libzstd-dev

WORKDIR /
# TODO do we need the samples?
RUN mkdir samples
COPY --from=builder /rudder-producer/samples samples
COPY --from=builder /rudder-producer/rudder-ingester .

CMD ["/rudder-producer"]
