# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy and cache dependencies first for better layer caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy only necessary source code
COPY cmd/ ./cmd/
COPY config/ ./config/
COPY log/ ./log/
COPY sidecar/ ./sidecar/
COPY main.go ./

# Build with cache mounts for faster rebuilds
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o fastlane-sidecar .

# Final stage - minimal runtime image
FROM alpine:latest

# Install only runtime dependencies
RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/fastlane-sidecar .

# Ensure binary is executable
RUN chmod +x fastlane-sidecar

# Switch to non-root user
USER appuser

ENTRYPOINT ["./fastlane-sidecar"]
