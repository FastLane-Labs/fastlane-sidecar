#!/bin/bash

set -e

# Configuration
PACKAGE_NAME="fastlane-generate-envelope"
# Version must be provided as first argument
if [ -z "$1" ]; then
    echo "Error: VERSION argument is required"
    echo "Usage: $0 <version>"
    exit 1
fi
VERSION=$1
ARCH="amd64"
BUILD_DIR="build-generate-envelope"
DEB_DIR="${BUILD_DIR}/debian"

echo "Building ${PACKAGE_NAME} v${VERSION} for ${ARCH}"

# Clean and create build directory
rm -rf "${BUILD_DIR}"
mkdir -p "${DEB_DIR}"

# Create package structure
mkdir -p "${DEB_DIR}/DEBIAN"
mkdir -p "${DEB_DIR}/usr/bin"

# Build the Go binary
echo "Building Go binary..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X github.com/FastLane-Labs/fastlane-sidecar/pkg/version.Version=${VERSION}" \
    -o "${DEB_DIR}/usr/bin/fastlane-generate-envelope" ./cmd/generate-envelope

# Make binary executable
chmod +x "${DEB_DIR}/usr/bin/fastlane-generate-envelope"

# Copy control file and update version
cp debian/generate-envelope/control "${DEB_DIR}/DEBIAN/control"
sed -i "s/Version: .*/Version: ${VERSION}/" "${DEB_DIR}/DEBIAN/control"

# Build the .deb package
echo "Building .deb package..."
dpkg-deb --build "${DEB_DIR}" "${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"

echo "Package built successfully: ${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"

# Show package info
echo "Package info:"
dpkg-deb --info "${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"
