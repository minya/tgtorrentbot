# One Dockerfile for the three images; pick one with --target bot|webapp|mcp
# (make bot-image, webapp-image, mcp-image; docker-compose.yaml sets target).
# BuildKit builds only the stages a target needs; without --target the build fails.

FROM golang:1.25-alpine AS src
WORKDIR /app
COPY . .

# The cache mounts persist between builds and are shared by the three
# binaries, so only changed packages recompile.
FROM src AS build-bot
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/tgtorrentbot ./cmd/tgtorrentbot

FROM src AS build-webapp
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/tgtorrentbot-webapp ./cmd/tgtorrentbot-webapp

FROM src AS build-mcp
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/tgtorrentbot-mcp ./cmd/tgtorrentbot-mcp

# uid/gid 1000: all three services write to the mounted downloads (the
# bot's auto-fixer and the MCP server rewrite tags and rename folders, the
# webapp's "Remove data" deletes them), so they must own them.
FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser
WORKDIR /app
USER appuser

FROM runtime AS bot
COPY --from=build-bot /out/tgtorrentbot ./tgtorrentbot
EXPOSE 80
CMD ["/app/tgtorrentbot"]

# Static assets are embedded (go:embed), so there is no separate COPY.
FROM runtime AS webapp
COPY --from=build-webapp /out/tgtorrentbot-webapp ./tgtorrentbot-webapp
EXPOSE 8080
CMD ["/app/tgtorrentbot-webapp"]

FROM runtime AS mcp
COPY --from=build-mcp /out/tgtorrentbot-mcp ./tgtorrentbot-mcp
EXPOSE 8080
CMD ["/app/tgtorrentbot-mcp"]

# Last, so a build without --target stops here instead of making the mcp
# image (which `docker build -t tgtorrentbot_img .` would put in the bot's
# container).
FROM alpine:3.22 AS no-target
RUN echo "choose an image: --target bot, webapp or mcp" >&2 && exit 1
