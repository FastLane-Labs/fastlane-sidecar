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

The service uses the default home directory `/home/monad/fastlane/`, which creates:
- `/home/monad/fastlane/node_to_sidecar` (node → sidecar)
- `/home/monad/fastlane/sidecar_to_node` (sidecar → node)

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
  -network=testnet-2 \
  -log-level=info \
  -gateway-url=https://gateway.example.com
```

### Configuration Flags

- `-network` - Network name: testnet, testnet-2, mainnet (default: `testnet`)
- `-fastlane-contract` - Override fastlane contract address (optional, uses network default if not set)
- `-home` - Fastlane home directory (default: `/home/monad/fastlane/`)
- `-gateway-url` - Override HTTP URL for MEV gateway (optional, uses network default if not set)
- `-log-level` - Log level: debug, info, warn, error (default: `debug`)
- `-pool-max-duration-ms` - Max time to hold transactions in pool (default: `60000`)
- `-auction-cycle-ms` - Auction cycle interval (default: `200`)
- `-delegation` - Delegation envelope JSON filename relative to home (default: `delegation-envelope.json`)
- `-keystore` - Sidecar keystore filename relative to home (default: `sidecar-keystore.json`)
- `-password-file` - Path to file containing keystore password (optional)
- `-disable-gateway-ingress` - Disable receiving transactions from gateway (default: `false`)
- `-disable-gateway-egress` - Disable sending transactions to gateway (default: `false`)

### Example systemd Configuration

```bash
sudo systemctl edit fastlane-sidecar

# Add:
[Service]
ExecStart=
ExecStart=/usr/bin/fastlane-sidecar \
  -log-level=info \
  -network=testnet-2 \
  -gateway-url=https://gateway.example.com
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

### 2. Delegation Envelope Generator

A utility tool to generate delegation envelopes and sidecar keystores for authentication with the MEV gateway.

**Build:**
```bash
# Build the generator binary
go build -o generate-envelope ./cmd/generate-envelope

# Run the tool
./generate-envelope --help
```

**Usage Examples:**

Generate a delegation envelope with a new sidecar keystore:
```bash
# Generate unsigned delegation (requires gateway approval)
./generate-envelope \
  --network testnet \
  --home /home/monad/fastlane \
  --validator-pubkey 0x03abc...def \
  --sidecar-password "your-secure-password"

# Generate signed delegation with validator keystore
./generate-envelope \
  --network testnet \
  --home /home/monad/fastlane \
  --validator-keystore /path/to/validator-keystore.json \
  --sidecar-password "your-secure-password"
```

**Output Files:**
- `delegation-envelope.json` - Delegation document for MEV gateway authentication
- `sidecar-keystore.json` - Encrypted keystore containing the sidecar's private key

**Available Flags:**
- `--home` - FastLane home directory (default: `/home/monad/fastlane`)
- `--network` - Network (testnet, testnet-2, mainnet) (default: `testnet`)
- `--validator-pubkey` - Validator public key (compressed, 33 bytes, 0x-prefixed)
- `--validator-keystore` - Path to validator keystore file for signed delegations
- `--validator-password` - Password for validator keystore (will prompt if not provided)
- `--sidecar-password` - Password for sidecar keystore (required)
- `--output` - Output delegation envelope file (default: `<home>/delegation-envelope.json`)

**Note:** Use either `--validator-pubkey` (unsigned) OR `--validator-keystore` (signed), not both.

## Health Endpoint

The sidecar exposes a health endpoint for monitoring its status.

**Endpoint:** `GET http://localhost:8765/health`

**Response Format:**
```json
{
  "last_heartbeat": "2025-10-15T12:34:56Z",
  "tx_received": 1234,
  "tx_streamed": 567,
  "pool_size": 89,
  "gateway_connected": true,
  "gateway_authenticated": true,
  "gateway_error": "",
  "timestamp": "2025-10-15T12:34:56Z"
}
```

**Field Descriptions:**

- `last_heartbeat` - Timestamp of last heartbeat received from the node
- `tx_received` - Total count of transactions received by the sidecar
- `tx_streamed` - Number of transactions streamed back to the node with priority
- `pool_size` - Current number of transactions in the transaction pool
- `gateway_connected` - Whether the gateway client is connected
- `gateway_authenticated` - Whether the sidecar is authenticated with the gateway
- `gateway_error` - Error message if gateway connection failed (omitted if no error)
- `timestamp` - Current time when the health check was performed

**Monitoring Examples:**

```bash
# Check health status
curl http://localhost:8765/health | jq

# Monitor node connectivity (check if heartbeat is recent)
curl -s http://localhost:8765/health | jq '.last_heartbeat'

# Check gateway status
curl -s http://localhost:8765/health | jq '{connected: .gateway_connected, authenticated: .gateway_authenticated, error: .gateway_error}'

# Monitor transaction flow
curl -s http://localhost:8765/health | jq '{received: .tx_received, streamed: .tx_streamed, pool: .pool_size}'
```

**Health Indicators:**

- **Node Health:** `last_heartbeat` should be recent (within last few seconds)
- **Gateway Health:** `gateway_connected=true` and `gateway_authenticated=true` indicates full operation
- **Transaction Flow:** `tx_received` should increase as transactions arrive; `tx_streamed` shows prioritized transactions sent to node