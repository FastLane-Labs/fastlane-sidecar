#!/bin/bash

set -e

# Configuration
PACKAGE_NAME="fastlane-sidecar"
# Version must be provided as first argument
if [ -z "$1" ]; then
    echo "Error: VERSION argument is required"
    echo "Usage: $0 <version>"
    exit 1
fi
VERSION=$1
ARCH="amd64"
BUILD_DIR="build"
DEB_DIR="${BUILD_DIR}/debian"

echo "Building ${PACKAGE_NAME} v${VERSION} for ${ARCH}"

# Check for systemd development libraries
if ! pkg-config --exists libsystemd; then
    echo "Error: libsystemd-dev is required for building"
    echo "Install with: sudo apt-get install libsystemd-dev"
    exit 1
fi

# Clean and create build directory
rm -rf "${BUILD_DIR}"
mkdir -p "${DEB_DIR}"

# Create package structure
mkdir -p "${DEB_DIR}/DEBIAN"
mkdir -p "${DEB_DIR}/usr/bin"
mkdir -p "${DEB_DIR}/lib/systemd/system"

# Build the Go binary
echo "Building Go binary..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o "${DEB_DIR}/usr/bin/fastlane-sidecar" ./cmd/sidecar

# Make binary executable
chmod +x "${DEB_DIR}/usr/bin/fastlane-sidecar"

# Copy control file and update version
cp debian/sidecar/control "${DEB_DIR}/DEBIAN/control"
sed -i "s/Version: .*/Version: ${VERSION}/" "${DEB_DIR}/DEBIAN/control"

# Copy systemd service file
cp debian/sidecar/fastlane-sidecar.service "${DEB_DIR}/lib/systemd/system/"

# Copy and make scripts executable
cp debian/sidecar/postinst "${DEB_DIR}/DEBIAN/"
cp debian/sidecar/postrm "${DEB_DIR}/DEBIAN/"
chmod +x "${DEB_DIR}/DEBIAN/postinst"
chmod +x "${DEB_DIR}/DEBIAN/postrm"

# Build the .deb package
echo "Building .deb package..."
dpkg-deb --build "${DEB_DIR}" "${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"

echo "Package built successfully: ${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"

# Show package info
echo "Package info:"
dpkg-deb --info "${BUILD_DIR}/${PACKAGE_NAME}_${VERSION}_${ARCH}.deb"
