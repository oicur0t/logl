#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "  logl Tailer Deployment Script"
echo "=========================================="
echo ""

# Check if running as root
if [ "$EUID" -eq 0 ]; then
    echo -e "${RED}Please don't run this script as root${NC}"
    exit 1
fi

# Step 1: Check prerequisites
echo -e "${YELLOW}Step 1: Checking prerequisites...${NC}"
if ! command -v podman &> /dev/null; then
    echo -e "${RED}✗ Podman not found${NC}"
    echo "Please install Podman first"
    exit 1
fi
echo -e "${GREEN}✓ Podman found${NC}"
echo ""

# Step 2: Get server details
echo -e "${YELLOW}Step 2: Server configuration${NC}"
read -p "Enter logl-server hostname/IP: " SERVER_HOST
read -p "Enter logl-server port [8443]: " SERVER_PORT
SERVER_PORT=${SERVER_PORT:-8443}
echo ""

# Step 3: Service configuration
echo -e "${YELLOW}Step 3: Service configuration${NC}"
read -p "Enter service name (e.g., web-api, auth-service): " SERVICE_NAME
if [ -z "$SERVICE_NAME" ]; then
    echo -e "${RED}Service name is required${NC}"
    exit 1
fi
echo ""

# Step 4: Certificate setup
echo -e "${YELLOW}Step 4: Certificate setup${NC}"
CERT_DIR="$HOME/.logl/certs"
mkdir -p "$CERT_DIR"

if [ ! -f "$CERT_DIR/ca.crt" ] || [ ! -f "$CERT_DIR/client.crt" ] || [ ! -f "$CERT_DIR/client.key" ]; then
    echo "Certificates not found in $CERT_DIR"
    echo "You need to copy the following files from your logl-server:"
    echo "  - ca.crt"
    echo "  - client.crt"
    echo "  - client.key"
    echo ""
    echo "From the server, run:"
    echo "  cd ~/logl/deployments/certs/certs"
    echo "  tar czf tailer-certs.tar.gz ca.crt client.crt client.key"
    echo "  scp tailer-certs.tar.gz $(whoami)@$(hostname -I | awk '{print $1}'):~/"
    echo ""
    read -p "Have you copied the certificates? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Please copy certificates and run this script again"
        exit 1
    fi

    if [ -f "$HOME/tailer-certs.tar.gz" ]; then
        tar xzf "$HOME/tailer-certs.tar.gz" -C "$CERT_DIR"
        chmod 644 "$CERT_DIR"/*
        echo -e "${GREEN}✓ Certificates extracted${NC}"
    else
        echo -e "${RED}Certificate tarball not found at $HOME/tailer-certs.tar.gz${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ Certificates found${NC}"
fi
echo ""

# Step 5: Create configuration
echo -e "${YELLOW}Step 5: Creating configuration${NC}"
CONFIG_DIR="$HOME/.logl"
mkdir -p "$CONFIG_DIR"

cat > "$CONFIG_DIR/tailer.yaml" <<EOF
service_name: "$SERVICE_NAME"
hostname: "\${HOSTNAME}"

log_files:
  - path: "/var/log/syslog"
    enabled: true
  - path: "/var/log/auth.log"
    enabled: true

server:
  url: "https://$SERVER_HOST:$SERVER_PORT/v1/logs/ingest"
  timeout: 30s
  max_retries: 5
  retry_backoff: 1s

batching:
  max_size: 100
  max_wait: 5s
  queue_size: 1000

mtls:
  ca_cert: "/etc/logl/certs/ca.crt"
  client_cert: "/etc/logl/certs/client.crt"
  client_key: "/etc/logl/certs/client.key"
  server_name: "logl-server"

state_file: "/var/lib/logl/tailer-state.json"
log_level: "info"
log_format: "json"
EOF

echo -e "${GREEN}✓ Configuration created at $CONFIG_DIR/tailer.yaml${NC}"
echo "You can edit this file to add more log files to monitor"
echo ""

# Step 6: Pull/build image
echo -e "${YELLOW}Step 6: Container image${NC}"
IMAGE_NAME="ghcr.io/oicur0t/logl-tailer:latest"

echo "Choose image option:"
echo "  1) Pull from GitHub Container Registry (recommended)"
echo "  2) Build locally from source"
read -p "Enter choice [1]: " IMAGE_CHOICE
IMAGE_CHOICE=${IMAGE_CHOICE:-1}

if [ "$IMAGE_CHOICE" = "2" ]; then
    if [ ! -d "$HOME/logl" ]; then
        echo "Cloning repository..."
        git clone https://github.com/oicur0t/logl.git "$HOME/logl"
    else
        echo "Updating repository..."
        cd "$HOME/logl" && git pull
    fi

    echo "Building image..."
    podman build -f "$HOME/logl/deployments/podman/Dockerfile.tailer" -t logl-tailer:latest "$HOME/logl"
    IMAGE_NAME="localhost/logl-tailer:latest"
else
    echo "Pulling image from registry..."
    podman pull "$IMAGE_NAME" || {
        echo -e "${YELLOW}Warning: Could not pull from registry, building locally instead${NC}"
        if [ ! -d "$HOME/logl" ]; then
            git clone https://github.com/oicur0t/logl.git "$HOME/logl"
        fi
        podman build -f "$HOME/logl/deployments/podman/Dockerfile.tailer" -t logl-tailer:latest "$HOME/logl"
        IMAGE_NAME="localhost/logl-tailer:latest"
    }
fi
echo -e "${GREEN}✓ Image ready${NC}"
echo ""

# Step 7: Stop existing container if any
echo -e "${YELLOW}Step 7: Checking for existing container${NC}"
if podman ps -a --format "{{.Names}}" | grep -q "^logl-tailer$"; then
    echo "Stopping and removing existing logl-tailer container..."
    podman stop logl-tailer 2>/dev/null || true
    podman rm logl-tailer 2>/dev/null || true
    echo -e "${GREEN}✓ Existing container removed${NC}"
fi
echo ""

# Step 8: Run container
echo -e "${YELLOW}Step 8: Starting logl-tailer container${NC}"
podman run -d \
  --name logl-tailer \
  -e HOSTNAME="$(hostname)" \
  -v "$CERT_DIR:/etc/logl/certs:ro" \
  -v "$CONFIG_DIR/tailer.yaml:/etc/logl/tailer.yaml:ro" \
  -v /var/log:/var/log:ro \
  --restart unless-stopped \
  "$IMAGE_NAME"

echo -e "${GREEN}✓ Container started${NC}"
echo ""

# Step 9: Verify
echo -e "${YELLOW}Step 9: Verifying deployment${NC}"
sleep 3
if podman ps | grep -q logl-tailer; then
    echo -e "${GREEN}✓ Container is running${NC}"
    echo ""
    echo "Checking logs..."
    podman logs logl-tailer 2>&1 | tail -10
    echo ""
    echo -e "${GREEN}=========================================="
    echo "  Deployment Complete!"
    echo "==========================================${NC}"
    echo ""
    echo "Useful commands:"
    echo "  View logs:    podman logs -f logl-tailer"
    echo "  Stop:         podman stop logl-tailer"
    echo "  Start:        podman start logl-tailer"
    echo "  Remove:       podman rm -f logl-tailer"
    echo "  Edit config:  nano $CONFIG_DIR/tailer.yaml"
    echo ""
else
    echo -e "${RED}✗ Container failed to start${NC}"
    echo "Checking logs..."
    podman logs logl-tailer
    exit 1
fi
