FROM golang:1.24.3 AS build


WORKDIR /app

#COPY go.mod ./
#COPY go.sum ./
#
#COPY *.go ./
#COPY ./rutracker ./rutracker
COPY . ./
RUN go mod download
RUN go mod tidy

VOLUME [ "/app" ]

RUN mkdir -p /out
RUN go build -o /out ./...

FROM golang:alpine

RUN apk add libc6-compat && apk cache clean

VOLUME /var/log
VOLUME /root

COPY --from=build /out/tgtorrentbot /root/app/tgtorrentbot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /root/app/
CMD ["/root/app/tgtorrentbot"]
