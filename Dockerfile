# syntax=docker/dockerfile:1

##
## Build
##
FROM golang:1.17-buster AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /tgtorrentbot

##
## Deploy
##
FROM gcr.io/distroless/base-debian10

WORKDIR /

COPY --from=build /tgtorrentbot /tgtorrentbot

USER nonroot:nonroot

ENTRYPOINT ["/tgtorrentbot"]

