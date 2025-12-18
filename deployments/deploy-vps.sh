#!/bin/bash
# Automated deployment script for logl on VPS
# This script performs a complete deployment with interactive prompts

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}logl VPS Deployment Automation${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Change to script directory
cd "$(dirname "$0")/.."

# Step 1: Verify prerequisites
echo -e "${YELLOW}Step 1: Verifying prerequisites...${NC}"
if [ -f "deployments/verify-prerequisites.sh" ]; then
    chmod +x deployments/verify-prerequisites.sh
    if ! ./deployments/verify-prerequisites.sh; then
        echo -e "${RED}Prerequisites check failed. Please fix errors before continuing.${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}Warning: Verification script not found, skipping checks${NC}"
fi
echo ""

# Step 2: Check if podman compose is available
echo -e "${YELLOW}Step 2: Checking podman compose...${NC}"
if podman compose version &> /dev/null; then
    COMPOSE_VERSION=$(podman compose version 2>&1 || echo "unknown")
    echo -e "${GREEN}✓ podman compose available${NC}"
    echo "  Version info: $COMPOSE_VERSION"
else
    echo -e "${RED}✗ podman compose not available${NC}"
    echo "Your Podman version doesn't support the compose subcommand."
    echo "Please upgrade Podman or install podman-compose separately."
    exit 1
fi
echo ""

# Step 3: Generate mTLS certificates
echo -e "${YELLOW}Step 3: Checking mTLS certificates...${NC}"
if [ ! -f "deployments/certs/certs/ca.crt" ]; then
    echo "mTLS certificates not found"
    read -p "Do you want to generate certificates now? (Y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        make certs
        echo -e "${GREEN}✓ Certificates generated${NC}"
    else
        echo -e "${RED}Certificates are required. Exiting.${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ Certificates already exist${NC}"

    # Check certificate expiration
    EXPIRY_DATE=$(openssl x509 -enddate -noout -in deployments/certs/certs/server.crt | cut -d= -f2)
    EXPIRY_EPOCH=$(date -d "$EXPIRY_DATE" +%s 2>/dev/null || date -j -f "%b %d %T %Y %Z" "$EXPIRY_DATE" +%s 2>/dev/null || echo 0)
    NOW_EPOCH=$(date +%s)
    DAYS_LEFT=$(( ($EXPIRY_EPOCH - $NOW_EPOCH) / 86400 ))

    if [ $DAYS_LEFT -lt 30 ]; then
        echo -e "${YELLOW}⚠  Warning: Certificate expires in $DAYS_LEFT days${NC}"
        read -p "Do you want to regenerate certificates? (y/N) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            make certs
            echo -e "${GREEN}✓ Certificates regenerated${NC}"
        fi
    else
        echo -e "${GREEN}  Certificate valid for $DAYS_LEFT more days${NC}"
    fi
fi
echo ""

# Step 4: Check configuration files
echo -e "${YELLOW}Step 4: Checking configuration files...${NC}"

# Check server config
if [ ! -f "deployments/podman/server.yaml" ]; then
    echo -e "${YELLOW}⚠  Server configuration not found${NC}"
    if [ -f "configs/server.example.yaml" ]; then
        echo "Copying example configuration..."
        cp configs/server.example.yaml deployments/podman/server.yaml
        echo -e "${YELLOW}Please edit deployments/podman/server.yaml with your MongoDB credentials${NC}"
        read -p "Press Enter when ready to continue..."
    else
        echo -e "${RED}Example configuration not found. Please create server.yaml manually.${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ Server configuration exists${NC}"
fi

# Check tailer config
if [ ! -f "deployments/podman/tailer.yaml" ]; then
    echo -e "${YELLOW}⚠  Tailer configuration not found${NC}"
    if [ -f "configs/tailer.example.yaml" ]; then
        echo "Copying example configuration..."
        cp configs/tailer.example.yaml deployments/podman/tailer.yaml
        echo -e "${YELLOW}Please edit deployments/podman/tailer.yaml with your service name and log paths${NC}"
        read -p "Press Enter when ready to continue..."
    else
        echo -e "${RED}Example configuration not found. Please create tailer.yaml manually.${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ Tailer configuration exists${NC}"
fi

# Verify MongoDB certificate
if [ ! -f "/certificates/mongodb-cert.pem" ]; then
    echo -e "${YELLOW}⚠  MongoDB certificate not found at /certificates/mongodb-cert.pem${NC}"
    echo "Server deployment will fail without this certificate."
    read -p "Do you want to continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${RED}Exiting. Please place MongoDB certificate at /certificates/mongodb-cert.pem${NC}"
        exit 1
    fi
else
    echo -e "${GREEN}✓ MongoDB certificate exists${NC}"
fi
echo ""

# Step 5: Build container images
echo -e "${YELLOW}Step 5: Building container images...${NC}"
read -p "Do you want to build container images now? (Y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    make docker-build
    echo -e "${GREEN}✓ Container images built${NC}"
else
    echo -e "${YELLOW}Skipping image build${NC}"
fi
echo ""

# Step 6: Configure firewall
echo -e "${YELLOW}Step 6: Firewall configuration...${NC}"
if command -v ufw &> /dev/null; then
    UFW_STATUS=$(sudo ufw status | grep "8443" || echo "not configured")
    if [[ "$UFW_STATUS" == "not configured" ]]; then
        echo "Port 8443 is not open in firewall"
        read -p "Do you want to open port 8443? (Y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            sudo ufw allow 8443/tcp
            sudo ufw reload
            echo -e "${GREEN}✓ Port 8443 opened${NC}"
        fi
    else
        echo -e "${GREEN}✓ Port 8443 is already open${NC}"
    fi
elif command -v iptables &> /dev/null; then
    if sudo iptables -L INPUT -n | grep -q "dpt:8443"; then
        echo -e "${GREEN}✓ Port 8443 is already open${NC}"
    else
        echo "Port 8443 is not open in firewall"
        read -p "Do you want to open port 8443? (Y/n) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Nn]$ ]]; then
            sudo iptables -A INPUT -p tcp --dport 8443 -j ACCEPT
            if [ -d /etc/iptables ]; then
                sudo iptables-save | sudo tee /etc/iptables/rules.v4 >/dev/null
            fi
            echo -e "${GREEN}✓ Port 8443 opened${NC}"
        fi
    fi
else
    echo -e "${YELLOW}⚠  Cannot detect firewall, skipping${NC}"
fi
echo ""

# Step 7: Deploy services
echo -e "${YELLOW}Step 7: Deploying services...${NC}"
read -p "Do you want to start logl services now? (Y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    # Stop any existing containers
    if podman ps -a | grep -q "logl-"; then
        echo "Stopping existing logl containers..."
        make stop-local 2>/dev/null || true
    fi

    # Start services
    make run-local
    echo -e "${GREEN}✓ Services started${NC}"

    # Wait a few seconds for services to initialize
    echo "Waiting for services to initialize..."
    sleep 5

    # Check container status
    echo ""
    echo "Container status:"
    podman ps | grep -E "(CONTAINER|logl-)" || echo "No containers found"
    echo ""
else
    echo -e "${YELLOW}Skipping service deployment${NC}"
    echo ""
    echo "To deploy later, run:"
    echo "  make run-local"
    exit 0
fi

# Step 8: Verify deployment
echo -e "${YELLOW}Step 8: Verifying deployment...${NC}"

# Check if containers are running
if podman ps | grep -q "logl-server"; then
    echo -e "${GREEN}✓ logl-server container is running${NC}"
else
    echo -e "${RED}✗ logl-server container is not running${NC}"
    echo "Check logs with: podman logs logl-server"
fi

if podman ps | grep -q "logl-tailer"; then
    echo -e "${GREEN}✓ logl-tailer container is running${NC}"
else
    echo -e "${RED}✗ logl-tailer container is not running${NC}"
    echo "Check logs with: podman logs logl-tailer"
fi

# Test health endpoint
echo ""
echo "Testing health endpoint..."
sleep 2  # Give server a moment to start

if curl -k -s https://localhost:8443/v1/health | grep -q "healthy"; then
    echo -e "${GREEN}✓ Server health check passed${NC}"
else
    echo -e "${YELLOW}⚠  Health check failed or server not ready yet${NC}"
    echo "  Try: curl -k https://localhost:8443/v1/health"
fi

# Check server logs for MongoDB connection
echo ""
echo "Checking MongoDB connection..."
if podman logs logl-server 2>&1 | grep -q "Connected to MongoDB"; then
    echo -e "${GREEN}✓ MongoDB connection successful${NC}"
else
    echo -e "${YELLOW}⚠  MongoDB connection not confirmed${NC}"
    echo "  Check logs with: podman logs logl-server"
fi

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Deployment Complete!${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo "Useful commands:"
echo "  View server logs:  podman logs -f logl-server"
echo "  View tailer logs:  podman logs -f logl-tailer"
echo "  Container status:  podman ps"
echo "  Stop services:     make stop-local"
echo "  Restart services:  make stop-local && make run-local"
echo ""
echo "Next steps:"
echo "  1. Monitor logs to ensure log ingestion is working"
echo "  2. Check MongoDB Atlas for incoming logs"
echo "  3. Deploy tailers on additional application hosts (see deployments/TAILER_DEPLOYMENT.md)"
echo "  4. Set up systemd service for auto-start (see deployments/VPS_DEPLOYMENT.md)"
echo ""
