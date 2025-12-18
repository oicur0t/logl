#!/bin/bash
# Pre-deployment verification script for logl
# Checks all prerequisites before deployment

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================"
echo "logl Pre-Deployment Verification"
echo "========================================"
echo ""

# Track overall status
ERRORS=0
WARNINGS=0

# Function to check command exists
check_command() {
    if command -v "$1" &> /dev/null; then
        echo -e "${GREEN}✓${NC} $1 found: $(command -v $1)"
        return 0
    else
        echo -e "${RED}✗${NC} $1 not found"
        return 1
    fi
}

# Function to check file exists
check_file() {
    if [ -f "$1" ]; then
        echo -e "${GREEN}✓${NC} File exists: $1"
        return 0
    else
        echo -e "${RED}✗${NC} File missing: $1"
        return 1
    fi
}

# Function to check directory exists
check_directory() {
    if [ -d "$1" ]; then
        echo -e "${GREEN}✓${NC} Directory exists: $1"
        return 0
    else
        echo -e "${RED}✗${NC} Directory missing: $1"
        return 1
    fi
}

echo "1. Checking required commands..."
echo "--------------------------------"
check_command "podman" || ((ERRORS++))

# Check for podman compose (built-in subcommand)
if podman compose version &> /dev/null; then
    echo -e "${GREEN}✓${NC} podman compose found (built-in)"
else
    echo -e "${YELLOW}⚠${NC}  podman compose not available"
    ((WARNINGS++))
fi
check_command "go" || {
    echo -e "${YELLOW}⚠${NC}  Go not found (only needed for building from source)"
    ((WARNINGS++))
}
echo ""

echo "2. Checking Podman configuration..."
echo "------------------------------------"
if command -v podman &> /dev/null; then
    PODMAN_VERSION=$(podman --version)
    echo -e "${GREEN}✓${NC} Podman version: $PODMAN_VERSION"

    # Check if podman can run
    if podman ps &> /dev/null; then
        echo -e "${GREEN}✓${NC} Podman is operational"
    else
        echo -e "${RED}✗${NC} Podman cannot run (check permissions)"
        ((ERRORS++))
    fi
else
    echo -e "${RED}✗${NC} Podman not installed"
    ((ERRORS++))
fi
echo ""

echo "3. Checking MongoDB certificate..."
echo "-----------------------------------"
check_file "/certificates/mongodb-cert.pem" || {
    echo -e "${YELLOW}⚠${NC}  MongoDB certificate not found at /certificates/mongodb-cert.pem"
    echo "   You will need to provide this certificate for server deployment"
    ((WARNINGS++))
}
echo ""

echo "4. Checking configuration files..."
echo "-----------------------------------"
cd "$(dirname "$0")"
check_file "../deployments/podman/server.yaml" || {
    echo -e "${YELLOW}⚠${NC}  Server configuration not found"
    echo "   Copy from configs/server.example.yaml and customize"
    ((WARNINGS++))
}
check_file "../deployments/podman/tailer.yaml" || {
    echo -e "${YELLOW}⚠${NC}  Tailer configuration not found"
    echo "   Copy from configs/tailer.example.yaml and customize"
    ((WARNINGS++))
}
echo ""

echo "5. Checking mTLS certificates..."
echo "---------------------------------"
CERTS_DIR="../deployments/certs/certs"
if [ -d "$CERTS_DIR" ]; then
    check_file "$CERTS_DIR/ca.crt" || ((WARNINGS++))
    check_file "$CERTS_DIR/server.crt" || ((WARNINGS++))
    check_file "$CERTS_DIR/server.key" || ((WARNINGS++))
    check_file "$CERTS_DIR/client.crt" || ((WARNINGS++))
    check_file "$CERTS_DIR/client.key" || ((WARNINGS++))

    if [ -f "$CERTS_DIR/server.crt" ]; then
        EXPIRY=$(openssl x509 -enddate -noout -in "$CERTS_DIR/server.crt" 2>/dev/null | cut -d= -f2)
        echo "   Server certificate expires: $EXPIRY"
    fi
else
    echo -e "${YELLOW}⚠${NC}  mTLS certificates not generated yet"
    echo "   Run: make certs"
    ((WARNINGS++))
fi
echo ""

echo "6. Checking network connectivity..."
echo "------------------------------------"
# Check if MongoDB is reachable (only domain, not full connection)
if host cluster0.rrp7vpi.mongodb.net &> /dev/null; then
    echo -e "${GREEN}✓${NC} MongoDB Atlas domain resolves"
else
    echo -e "${YELLOW}⚠${NC}  Cannot resolve MongoDB Atlas domain"
    echo "   Check DNS and network connectivity"
    ((WARNINGS++))
fi
echo ""

echo "7. Checking firewall for required ports..."
echo "-------------------------------------------"
if command -v ss &> /dev/null; then
    if ss -tuln | grep -q ":8443"; then
        echo -e "${YELLOW}⚠${NC}  Port 8443 is already in use"
        ss -tuln | grep ":8443"
        ((WARNINGS++))
    else
        echo -e "${GREEN}✓${NC} Port 8443 is available"
    fi
else
    echo -e "${YELLOW}⚠${NC}  Cannot check port availability (ss not found)"
    ((WARNINGS++))
fi
echo ""

echo "========================================"
echo "Verification Summary"
echo "========================================"
echo -e "Errors:   ${RED}$ERRORS${NC}"
echo -e "Warnings: ${YELLOW}$WARNINGS${NC}"
echo ""

if [ $ERRORS -eq 0 ]; then
    echo -e "${GREEN}✓ System is ready for deployment${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Generate certificates (if not done): make certs"
    echo "  2. Review configuration files in deployments/podman/"
    echo "  3. Deploy: make run-local"
    exit 0
else
    echo -e "${RED}✗ Please fix errors before deploying${NC}"
    exit 1
fi
