# Tailer-Only Deployment Guide

Deploy logl-tailer to send logs to a central logl-server.

## Quick Start

### On Server (generate certificates)
```bash
cd ~/logl/deployments/certs/certs
tar czf tailer-certs.tar.gz ca.crt client.crt client.key
```

### On Tailer Machine
```bash
# Get deployment script
curl -O https://raw.githubusercontent.com/oicur0t/logl/main/deployments/deploy-tailer.sh
chmod +x deploy-tailer.sh

# Copy certificates from server
scp server:~/logl/deployments/certs/certs/tailer-certs.tar.gz ~/

# Run deployment
./deploy-tailer.sh
```

## What the Script Does

1. Checks Podman installation
2. Prompts for server details (IP/hostname, port)
3. Prompts for service name
4. Extracts certificates to ~/.logl/certs
5. Creates config at ~/.logl/tailer.yaml
6. Pulls or builds container image
7. Starts logl-tailer container

## Useful Commands

```bash
# View logs
podman logs -f logl-tailer

# Stop/start
podman stop logl-tailer
podman start logl-tailer

# Edit configuration
nano ~/.logl/tailer.yaml
podman restart logl-tailer

# Remove
podman rm -f logl-tailer
```

## Adding Custom Log Files

Edit `~/.logl/tailer.yaml`:
```yaml
log_files:
  - path: "/var/log/nginx/access.log"
    enabled: true
  - path: "/app/logs/app.log"
    enabled: true
```

Then restart: `podman restart logl-tailer`
