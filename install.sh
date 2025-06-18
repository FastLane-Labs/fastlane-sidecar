#!/bin/bash

set -euo pipefail

IMAGE="ghcr.io/fastlane-labs/fastlane-sidecar"
DEFAULT_CONTAINER_NAME="monad-execution"
USER_PROVIDED_NAME="${1:-}"

# Resolve container name → container ID
if docker ps --filter "name=$DEFAULT_CONTAINER_NAME" --format "{{.ID}}" | grep -q .; then
  CONTAINER_NAME="$DEFAULT_CONTAINER_NAME"
elif [[ -n "$USER_PROVIDED_NAME" ]] && docker ps --filter "name=$USER_PROVIDED_NAME" --format "{{.ID}}" | grep -q .; then
  CONTAINER_NAME="$USER_PROVIDED_NAME"
else
  echo "⚠️  No running container named '$DEFAULT_CONTAINER_NAME' or '$USER_PROVIDED_NAME' found."
  echo "ℹ️  Please start a container before running this script."
  exit 1
fi

CONTAINER_ID=$(docker ps --filter "name=$CONTAINER_NAME" --format "{{.ID}}" | head -n 1)
echo "✅ Using container ID: $CONTAINER_ID"

# Pull latest image
echo "📦 Pulling latest image: $IMAGE"
docker pull "$IMAGE"

# Run sidecar
echo "🚀 Starting fastlane-sidecar"
docker run --rm -it \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 8080:8080 \
  --name sidecar-runner \
  "$IMAGE" \
  -docker-container-id "$CONTAINER_ID"
