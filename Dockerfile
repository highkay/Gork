# Builder
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS builder

RUN apk add --no-cache ca-certificates git

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY app ./app
COPY cmd ./cmd

RUN target_os="${TARGETOS:-$(go env GOOS)}" \
    && target_arch="${TARGETARCH:-$(go env GOARCH)}" \
    && CGO_ENABLED=0 GOOS="${target_os}" GOARCH="${target_arch}" go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gork \
    ./cmd/gork

# Runtime
FROM alpine:3.22

ENV TZ=Asia/Shanghai \
    SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8000 \
    SERVER_WORKERS=1 \
    DATA_DIR=/app/data \
    LOG_DIR=/app/logs \
    TMPDIR=/app/data/tmp \
    GORK_USER=gork \
    GORK_GROUP=gork

RUN apk add --no-cache \
    tzdata \
    ca-certificates \
    su-exec \
    && update-ca-certificates \
    && addgroup -S -g 10001 gork \
    && adduser -S -D -H -u 10001 -G gork gork

WORKDIR /app

COPY --from=builder /out/gork /app/gork
COPY config.defaults.toml ./
COPY app/statics ./app/statics
COPY scripts/entrypoint.sh scripts/init_storage.sh ./scripts/

RUN mkdir -p /app/data /app/logs \
    && chown -R gork:gork /app/data /app/logs \
    && chmod +x /app/gork /app/scripts/entrypoint.sh /app/scripts/init_storage.sh

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/app/gork", "healthcheck"]

ENTRYPOINT ["/app/scripts/entrypoint.sh"]
CMD ["sh", "-c", "HOST=${HOST:-${SERVER_HOST:-0.0.0.0}} PORT=${PORT:-${SERVER_PORT:-8000}} exec /app/gork"]
