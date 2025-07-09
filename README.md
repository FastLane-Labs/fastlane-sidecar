# Fastlane Sidecar

A sidecar implementation to be run alongside a Monad Validator boosting it's ability to capture MEV.

# Usage

## Docker

```bash
make build
make run CONTAINER_ID=a8fc3ade5a9c
```

## DEB Package

Build and install as a systemd service:

```bash
# Install build dependencies
sudo apt-get install libsystemd-dev

# Build package
make build-deb

# Install
sudo dpkg -i build/fastlane-sidecar_$(cat VERSION)_amd64.deb

# Configure (optional)
sudo systemctl edit fastlane-sidecar
# Add: Environment=DOCKER_CONTAINER_ID=your-container-id

# Start service
sudo systemctl start fastlane-sidecar

# View logs
sudo journalctl -u fastlane-sidecar -f
```

The package creates a `fastlane` user and runs the service with security hardening enabled.