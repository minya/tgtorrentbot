FROM golang:1.21.1 AS build


WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
COPY ./rutracker ./rutracker
VOLUME [ "/app" ]

RUN mkdir -p /out
RUN go build -o /out ./...

FROM golang:alpine

RUN apk add libc6-compat

VOLUME /var/log
VOLUME /root

COPY --from=build /out/tgtorrentbot /root/app/tgtorrentbot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /root/app/
CMD /root/app/tgtorrentbot
