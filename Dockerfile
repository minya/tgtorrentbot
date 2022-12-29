# syntax=docker/dockerfile:1

##
## Build
##
FROM golang:1.18-buster AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
COPY ./rutracker ./rutracker
VOLUME [ "/app" ]

RUN mkdir -p /out
RUN go build -o /out ./...

##
## Deploy
##
FROM gcr.io/distroless/base-debian10

WORKDIR /

COPY --from=build /out/tgtorrentbot /tgtorrentbot

USER nonroot:nonroot

ENTRYPOINT ["/tgtorrentbot"]

