# Multi-stage Dockerfile for Resilient Catalog Synchronization Engine
# Constraints: CGO_ENABLED=0 static binary, uncompressed runtime <= 25MB, non-root user, SQLite volume persistence.

# ------------------------------------------------------------------------------
# Stage 1: Build static Go binary
# ------------------------------------------------------------------------------
ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder

# Build arguments for multi-platform support
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# Pre-fetch dependencies (optimized caching layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code tree
COPY . .

# Build statically linked standalone server binary
# -trimpath strips host paths from debug information
# -ldflags="-s -w" strips symbol tables and DWARF debugging information
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w" \
    -buildvcs=false \
    -o /build/bin/server ./cmd/server

# ------------------------------------------------------------------------------
# Stage 2: Minimal hardened Alpine runtime (<= 25MB uncompressed)
# ------------------------------------------------------------------------------
FROM alpine:3.21

# Install CA certificates for outbound HTTPS and tzdata for standard time zones
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S -G appgroup -h /app -s /sbin/nologin appuser

WORKDIR /app

# Pre-create data directory with non-root ownership for SQLite WAL mode compatibility
RUN mkdir -p /app/data && \
    chown -R appuser:appgroup /app && \
    chmod 755 /app/data

# Copy static server binary from builder
COPY --from=builder /build/bin/server /app/server

# Switch to non-root user
USER appuser:appgroup

# Expose default HTTP listen port
EXPOSE 8080

# Expose volume mount point for persistent SQLite database
VOLUME ["/app/data"]

# Default environment configuration
ENV PORT=8080 \
    DB_PATH=/app/data/catalog.db

# Healthcheck leveraging BusyBox wget
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/jobs || exit 1

# Exec form ENTRYPOINT ensures signals (SIGTERM/SIGINT) pass directly to server process
ENTRYPOINT ["/app/server"]
CMD ["-db", "/app/data/catalog.db", "-port", "8080"]
