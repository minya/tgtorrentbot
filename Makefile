BOT_IMAGE_NAME=tgtorrentbot_img
WEBAPP_IMAGE_NAME=tgtorrentbot-webapp_img

binaries:
	@echo "Building binaries..."
	go build -o bin/ ./cmd/...

bot-image:
	@echo "Building bot image..."
	@docker-buildx build --tag $(BOT_IMAGE_NAME) -f Dockerfile.bot .

webapp-image:
	@echo "Building webapp image..."
	@docker-buildx build --tag $(WEBAPP_IMAGE_NAME) -f Dockerfile.webapp .

images: bot-image webapp-image

.DEFAULT_GOAL := binaries
