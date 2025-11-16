IMAGE_NAME=tgtorrentbot_img

image:
	@echo "Building image..."
	@docker-buildx build --tag $(IMAGE_NAME) .

binaries:
	@echo "Building binaries..."
	go build -o bin/ ./...
