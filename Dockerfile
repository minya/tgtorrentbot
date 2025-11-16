FROM golang:1.24-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/tgtorrentbot .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates libc6-compat

WORKDIR /root/app
COPY --from=build /out/tgtorrentbot ./tgtorrentbot

CMD ["/root/app/tgtorrentbot"]
