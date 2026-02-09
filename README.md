# Fastlane Sidecar

A sidecar implementation to be run alongside a Monad Validator boosting its ability to capture MEV.

## Installation

### Option 1: Docker (Recommended for Development)

#### Pull from GitHub Container Registry

```bash
# Pull the latest stable release
docker pull ghcr.io/fastlane-labs/fastlane-sidecar:latest

# Or pull a specific version
docker pull ghcr.io/fastlane-labs/fastlane-sidecar:1.0.0

# Run the container
# Mount the IPC socket from your Monad validator
docker run -d \
  --name fastlane-sidecar \
  -v /home/monad/monad-bft:/home/monad/monad-bft \
  ghcr.io/fastlane-labs/fastlane-sidecar:latest

# Or specify a custom home directory
docker run -d \

  --name fastlane-sidecar \
  -v /custom/path:/custom/path \
  ghcr.io/fastlane-labs/fastlane-sidecar:latest \
  -home=/custom/path/
```

#### Build from Source

```bash
# Build the Docker image
docker build -t fastlane-sidecar .

# Run the container (mounts default IPC socket)
docker run -d \
  --name fastlane-sidecar \
  -v /home/monad/monad-bft:/home/monad/monad-bft \
  fastlane-sidecar
```

### Option 2: Debian Package (Recommended for Production)

#### Install from APT Repository

**Step 1: Add FastLane APT Repository**

```bash
# Download and install the GPG key
sudo mkdir -p /etc/apt/keyrings
wget -qO - https://fastlane.xyz/apt/fastlane-apt-key.gpg.bin -O /etc/apt/keyrings/fastlane-labs.gpg

# Add the repository to your sources
echo "deb [signed-by=/etc/apt/keyrings/fastlane-labs.gpg] https://fastlane-apt-repo.s3.amazonaws.com stable main" | \
  sudo tee /etc/apt/sources.list.d/fastlane-labs.list

# Update package list
sudo apt update
```

**Step 2: Install FastLane Sidecar**

```bash
# Install the latest stable version
sudo apt install fastlane-sidecar

# Or install a specific version (e.g., 1.0.0)
sudo apt install fastlane-sidecar=1.0.0

# Or install a dev version (e.g., 0~dev.abc1234)
sudo apt install fastlane-sidecar=0~dev.abc1234

# The service is installed but not started automatically
```

**Upgrading to a Different Version**

```bash
# Upgrade to a specific stable version
sudo apt update
sudo apt install fastlane-sidecar=1.0.0 -y
sudo systemctl restart fastlane-sidecar

# Upgrade/downgrade to a dev version
sudo apt update
sudo apt install fastlane-sidecar=0~dev.abc1234 -y --allow-downgrades
sudo systemctl restart fastlane-sidecar

# Check installed version
dpkg -l | grep fastlane-sidecar
```

**Step 3: Configure the Service (Optional)**

The service uses the default home directory `/home/monad/fastlane/`.

If your Monad validator uses a different path, configure it:

```bash
# Edit the service configuration
sudo systemctl edit fastlane-sidecar

# Add the following in the editor that opens:
# [Service]
# ExecStart=
# ExecStart=/usr/bin/fastlane-sidecar -log-level=info -home=/your/custom/path/

# Save and exit (Ctrl+X, then Y, then Enter in nano)
```

**Step 4: Start and Enable the Service**

```bash
# Enable the service to start on boot
sudo systemctl enable fastlane-sidecar

# Start the service
sudo systemctl start fastlane-sidecar

# Check the service status
sudo systemctl status fastlane-sidecar

# View logs
sudo journalctl -u fastlane-sidecar -f
```

#### Install from GitHub Releases

```bash
# Download the .deb package from the latest release
wget https://github.com/FastLane-Labs/fastlane-sidecar/releases/download/v1.0.0/fastlane-sidecar_1.0.0_amd64.deb

# Install the package
sudo dpkg -i fastlane-sidecar_1.0.0_amd64.deb

# Follow steps 3-4 above to configure and start the service
```

#### Build from Source

```bash
# Install build dependencies
sudo apt-get update
sudo apt-get install -y libsystemd-dev golang-1.23

# Build the package (version auto-detected from git)
make build-deb

# Install (replace VERSION with your actual version, e.g., 1.0.0)
sudo dpkg -i build/fastlane-sidecar_*_amd64.deb

# Follow steps 3-4 above to configure and start the service
```

## Service Management

```bash
# Start the service
sudo systemctl start fastlane-sidecar

# Stop the service
sudo systemctl stop fastlane-sidecar

# Restart the service
sudo systemctl restart fastlane-sidecar

# Check service status
sudo systemctl status fastlane-sidecar

# Enable service on boot
sudo systemctl enable fastlane-sidecar

# Disable service on boot
sudo systemctl disable fastlane-sidecar

# View logs (follow mode)
sudo journalctl -u fastlane-sidecar -f

# View recent logs
sudo journalctl -u fastlane-sidecar -n 100
```

## Configuration

The sidecar can be configured for different networks and parameters:

```bash
fastlane-sidecar \
  -network=testnet \
  -log-level=info
```

### Configuration Flags

- `-network` - Network name: testnet, mainnet (default: `testnet`)
- `-fastlane-contract` - Override fastlane contract address (optional, uses network default if not set)
- `-home` - Fastlane home directory (default: `/home/monad/fastlane/`)
- `-log-level` - Log level: debug, info, warn, error (default: `debug`)
- `-pool-max-duration-ms` - Maximum time to hold transactions in pool (default: `2500`)
- `-monitoring-port` - HTTP port for monitoring endpoints (/health and /metrics) (default: `8765`)

### Example systemd Configuration

```bash
sudo systemctl edit fastlane-sidecar

# Add:
[Service]
ExecStart=
ExecStart=/usr/bin/fastlane-sidecar \
  -log-level=info \
  -network=mainnet
```

## Security

The Debian package:
- Runs as the `monad` user to access the Monad validator's IPC socket
- Runs with security hardening enabled (restricted privileges)
- Integrates with systemd for automatic restart on failure

**Note:** The service must run as the `monad` user because it needs access to the Unix sockets in `/home/monad/fastlane/`, which are owned by the `monad` user.

## Building from Source

The repository contains two programs:

### 1. Fastlane Sidecar

The main sidecar application that runs alongside a Monad validator.

**Prerequisites:**
```bash
# Install Go 1.23 or later
sudo apt-get update
sudo apt-get install -y golang-1.23
```

**Build:**
```bash
# Clone the repository
git clone https://github.com/FastLane-Labs/fastlane-sidecar.git
cd fastlane-sidecar

# Build the sidecar binary
go build -o fastlane-sidecar ./cmd/sidecar

# Run directly
./fastlane-sidecar -home=/home/monad/fastlane/ -log-level=info

# Or install to system
sudo cp fastlane-sidecar /usr/local/bin/
fastlane-sidecar -home=/home/monad/fastlane/
```

**Build with Docker:**
```bash
# Build the Docker image
docker build -t fastlane-sidecar .

# Run the container
docker run -d \
  --name fastlane-sidecar \
  -v /home/monad/fastlane:/home/monad/fastlane \
  fastlane-sidecar \
  -home=/home/monad/fastlane/
```

## Health Endpoint

The sidecar exposes a health endpoint for monitoring its status.

**Endpoint:** `GET http://localhost:8765/health`

**Response Format:**
```json
{
  "status": "ok",
  "tx_received": 1234,
  "tx_streamed": 567,
  "pool_size": 89,
  "last_received_at": "2025-10-15T12:34:55Z",
  "last_sent_at": "2025-10-15T12:34:56Z",
  "timestamp": "2025-10-15T12:34:56Z"
}
```

**Field Descriptions:**

- `status` - Health status ("ok")
- `tx_received` - Total count of transactions received by the sidecar
- `tx_streamed` - Number of transactions streamed back to the node with priority
- `pool_size` - Current number of transactions in the transaction pool
- `last_received_at` - Timestamp of last transaction received from node (omitted if none)
- `last_sent_at` - Timestamp of last prioritized transaction sent to node (omitted if none)
- `timestamp` - Current time when the health check was performed

**Monitoring Examples:**

```bash
# Check health status
curl http://localhost:8765/health | jq

# Monitor transaction flow
curl -s http://localhost:8765/health | jq '{received: .tx_received, streamed: .tx_streamed, pool: .pool_size}'

# Check activity timestamps
curl -s http://localhost:8765/health | jq '{last_received: .last_received_at, last_sent: .last_sent_at}'
```

**Health Indicators:**

- **Transaction Flow:** `tx_received` should increase as transactions arrive; `tx_streamed` shows prioritized transactions sent to node
- **Activity:** `last_received_at` and `last_sent_at` show recent activity timestamps