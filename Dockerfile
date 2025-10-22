# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Build argument to select which binary to build
ARG BINARY=sidecar
# Build argument for version (defaults to 'dev' if not provided)
ARG VERSION=dev

WORKDIR /app

# Copy and cache dependencies first for better layer caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy only necessary source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

# Build with cache mounts for faster rebuilds
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -a -installsuffix cgo \
    -ldflags "-X github.com/FastLane-Labs/fastlane-sidecar/pkg/version.Version=${VERSION}" \
    -o app ./cmd/${BINARY}

# Final stage - minimal runtime image
FROM alpine:latest

# Install only runtime dependencies
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/app .

# Ensure binary is executable
RUN chmod +x app

ENTRYPOINT ["./app"]
