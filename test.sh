#!/bin/bash


GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o out/ -a ./rutracker/cmd/rutracker
docker build -t tmp_tgt_img -f Dockerfile.test .
echo  "Image built successfully, running tests..."
# use dns to resolve the hostnames

docker run --rm --dns 8.8.8.8 tmp_tgt_img
