#!/bin/bash
# Install podman-compose on Ubuntu
# This script installs podman-compose when it's not available in repositories

set -e

echo "Installing podman-compose..."

# Check if podman is installed
if ! command -v podman &> /dev/null; then
    echo "Error: Podman is not installed. Please install Podman first."
    exit 1
fi

# Check if podman-compose is already installed
if command -v podman-compose &> /dev/null; then
    CURRENT_VERSION=$(podman-compose --version 2>&1 || echo "unknown")
    echo "podman-compose is already installed: $CURRENT_VERSION"
    read -p "Do you want to upgrade to the latest version? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 0
    fi
fi

# Check if pip3 is installed
if ! command -v pip3 &> /dev/null; then
    echo "Installing pip3..."
    sudo apt-get update
    sudo apt-get install -y python3-pip
fi

# Install podman-compose via pip
echo "Installing podman-compose from PyPI..."
sudo pip3 install --upgrade podman-compose

# Verify installation
if command -v podman-compose &> /dev/null; then
    VERSION=$(podman-compose --version)
    echo ""
    echo "✓ podman-compose installed successfully!"
    echo "  Version: $VERSION"
    echo ""
    echo "Test it with: podman-compose --help"
else
    echo ""
    echo "✗ Installation failed. Please install manually:"
    echo "  sudo pip3 install podman-compose"
    exit 1
fi
