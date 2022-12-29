IMAGE_NAME=tgtorrentbot_img

build_image:
	@echo "Building image..."
	@docker build -t $(IMAGE_NAME) .

binaries:
	@echo "Building binaries..."
	go build -o bin/ ./...