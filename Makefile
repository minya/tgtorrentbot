BOT_IMAGE_NAME=tgtorrentbot_img
WEBAPP_IMAGE_NAME=tgtorrentbot-webapp_img
MCP_IMAGE_NAME=tgtorrentbot-mcp_img

.PHONY: binaries bot-image webapp-image mcp-image images hooks

binaries:
	@echo "Building binaries..."
	go build -o bin/ ./cmd/...

bot-image:
	@echo "Building bot image..."
	@docker-buildx build --tag $(BOT_IMAGE_NAME) -f Dockerfile.bot .

webapp-image:
	@echo "Building webapp image..."
	@docker-buildx build --tag $(WEBAPP_IMAGE_NAME) -f Dockerfile.webapp .

mcp-image:
	@echo "Building mcp image..."
	@docker-buildx build --tag $(MCP_IMAGE_NAME) -f Dockerfile.mcp .

images: bot-image webapp-image mcp-image

# Use the repository's hooks (hooks/pre-commit blocks go.mod replace
# directives). Run once per clone.
hooks:
	git config core.hooksPath hooks

.DEFAULT_GOAL := binaries
