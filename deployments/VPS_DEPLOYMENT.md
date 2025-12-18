# logl VPS Deployment Guide (wrangl-hal)

This guide is specifically for deploying logl on the **wrangl-hal** VPS running Ubuntu Oracular (24.10) at OCH cloud.

## System Information

- **Hostname**: wrangl-hal
- **OS**: Ubuntu Oracular (24.10)
- **Provider**: OCH Cloud
- **Purpose**: Production VPS hosting multiple services
- **Container Runtime**: Podman (standardized)

---

## Prerequisites

### 1. Install Podman

If you encountered repository issues during installation, ensure your APT sources are correct:

```bash
# Fix repository sources
sudo tee /etc/apt/sources.list << 'EOF'
deb http://archive.ubuntu.com/ubuntu/ oracular main restricted universe multiverse
deb http://archive.ubuntu.com/ubuntu/ oracular-updates main restricted universe multiverse
deb http://archive.ubuntu.com/ubuntu/ oracular-backports main restricted universe multiverse
deb http://security.ubuntu.com/ubuntu/ oracular-security main restricted universe multiverse
EOF

# Update and install
sudo apt-get clean
sudo apt-get update
sudo apt-get install -y podman
```

Verify installation:
```bash
podman --version
podman compose version  # Built-in compose support
```

### 2. Install Additional Tools

```bash
# Git (if not installed)
sudo apt-get install -y git

# Make
sudo apt-get install -y make

# Go (for building from source)
sudo apt-get install -y golang-go
```

### 3. MongoDB Certificate Setup

The MongoDB X.509 certificate must be placed at `/certificates/mongodb-cert.pem`:

```bash
# Create certificates directory
sudo mkdir -p /certificates

# Copy your MongoDB certificate (replace with actual path)
sudo cp /path/to/your/mongodb-cert.pem /certificates/mongodb-cert.pem

# Set permissions
sudo chmod 644 /certificates/mongodb-cert.pem
```

**Important**: This certificate is required for connecting to MongoDB Atlas with X.509 authentication.

---

## Deployment Steps

### Step 1: Clone Repository

```bash
# Clone from GitHub
cd ~
git clone https://github.com/oicur0t/logl.git
cd logl
```

### Step 2: Verify Prerequisites

Run the verification script to check all prerequisites:

```bash
chmod +x deployments/verify-prerequisites.sh
./deployments/verify-prerequisites.sh
```

Fix any errors reported before proceeding.

### Step 3: Generate mTLS Certificates

Generate certificates for secure communication between tailer and server:

```bash
make certs
```

This creates certificates in `deployments/certs/certs/`:
- `ca.crt` - CA certificate
- `server.crt` / `server.key` - Server certificate and key
- `client.crt` / `client.key` - Client certificate and key

### Step 4: Configure Server

The server configuration is already set up at `deployments/podman/server.yaml` with your MongoDB credentials.

Review and adjust if needed:

```bash
nano deployments/podman/server.yaml
```

**Key settings**:
- MongoDB URI: `mongodb+srv://cluster0.rrp7vpi.mongodb.net/...`
- Database: `logl`
- Certificate path: `/certificates/mongodb-cert.pem`
- Listen address: `0.0.0.0:8443`
- Log retention: 30 days (TTL)

### Step 5: Configure Tailer

The tailer runs on the same host for testing, but in production it will run on application hosts.

Edit the tailer configuration:

```bash
nano deployments/podman/tailer.yaml
```

**Important changes**:
- `service_name`: Change from "my-app" to your actual service name
- `log_files`: Update paths to actual log files you want to tail
- `server.url`: Verify it points to `https://logl-server:8443/v1/logs/ingest`

Example:
```yaml
service_name: "wrangl-api"  # Change this
log_files:
  - path: "/var/log/nginx/access.log"  # Change to your logs
    enabled: true
  - path: "/var/log/nginx/error.log"
    enabled: true
```

### Step 6: Configure Firewall

Open port 8443 for the logl server:

```bash
# UFW (if installed)
sudo ufw allow 8443/tcp
sudo ufw reload

# Or iptables
sudo iptables -A INPUT -p tcp --dport 8443 -j ACCEPT
sudo iptables-save | sudo tee /etc/iptables/rules.v4
```

### Step 7: Build Container Images

Build the Podman images:

```bash
make docker-build
```

This creates two images:
- `logl-server:latest`
- `logl-tailer:latest`

Verify images:
```bash
podman images | grep logl
```

### Step 8: Deploy Services

Start the services using podman-compose:

```bash
make run-local
```

This will:
1. Start the logl-server container on port 8443
2. Start the logl-tailer container
3. Create a persistent volume for tailer state
4. Set up a bridge network for inter-container communication

### Step 9: Verify Deployment

Check container status:
```bash
podman ps
```

You should see:
```
CONTAINER ID  IMAGE               STATUS       PORTS                   NAMES
...           logl-server:latest  Up X minutes 0.0.0.0:8443->8443/tcp  logl-server
...           logl-tailer:latest  Up X minutes                         logl-tailer
```

Check server health:
```bash
curl -k https://localhost:8443/v1/health
```

Expected response:
```json
{"status":"healthy"}
```

View server logs:
```bash
podman logs -f logl-server
```

View tailer logs:
```bash
podman logs -f logl-tailer
```

### Step 10: Verify MongoDB Connection

Check server logs for successful MongoDB connection:

```bash
podman logs logl-server 2>&1 | grep -i mongo
```

You should see:
```json
{"level":"info","msg":"Connected to MongoDB","database":"logl"}
{"level":"info","msg":"MongoDB indexes ensured for collection: logs_..."}
```

### Step 11: Test Log Ingestion

Add a test log entry to one of your configured log files:

```bash
echo "$(date -Iseconds) TEST: logl ingestion test" >> /var/log/nginx/access.log
```

Check tailer logs for batch sending:
```bash
podman logs logl-tailer 2>&1 | grep "Batch sent"
```

Check server logs for successful insertion:
```bash
podman logs logl-server 2>&1 | grep "Batch inserted"
```

Verify in MongoDB:
```bash
# Use MongoDB Compass or mongosh to query the logs collection
# Collection name will be: logs_wrangl_api (based on your service_name)
```

---

## Production Considerations

### Enable Auto-Start on Reboot

Create systemd service for podman-compose:

```bash
sudo tee /etc/systemd/system/logl.service << 'EOF'
[Unit]
Description=logl Log Ingestion Service
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/home/USERNAME/logl/deployments/podman
ExecStart=/usr/bin/podman-compose up -d
ExecStop=/usr/bin/podman-compose down
User=USERNAME

[Install]
WantedBy=multi-user.target
EOF

# Replace USERNAME with your actual username
sudo sed -i "s/USERNAME/$(whoami)/g" /etc/systemd/system/logl.service

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable logl.service
sudo systemctl start logl.service
```

### Monitor Logs

Set up log rotation for podman logs:

```bash
sudo tee /etc/logrotate.d/podman-containers << 'EOF'
/var/lib/containers/storage/overlay-containers/*/userdata/ctr.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
EOF
```

### Backup Tailer State

The tailer state is persisted in a Podman volume. Back it up regularly:

```bash
# List volumes
podman volume ls

# Inspect tailer-state volume
podman volume inspect podman_tailer-state

# Backup (if needed)
podman run --rm -v podman_tailer-state:/source:ro -v $(pwd):/backup alpine tar czf /backup/tailer-state-backup-$(date +%Y%m%d).tar.gz -C /source .
```

### Update and Restart

When updating logl:

```bash
cd ~/logl
git pull origin main
make docker-build
make stop-local
make run-local
```

---

## Troubleshooting

### Container Won't Start

Check logs:
```bash
podman logs logl-server
podman logs logl-tailer
```

Common issues:
- **Certificate errors**: Verify paths in server.yaml and tailer.yaml
- **MongoDB connection failed**: Check certificate at /certificates/mongodb-cert.pem
- **Port already in use**: Check with `ss -tuln | grep 8443`

### No Logs Being Ingested

1. Check tailer configuration has correct log file paths
2. Verify log files are readable by the container:
   ```bash
   podman exec -it logl-tailer ls -la /var/log
   ```
3. Check tailer logs for errors:
   ```bash
   podman logs logl-tailer 2>&1 | grep -i error
   ```

### High Memory Usage

Adjust batching configuration in tailer.yaml:
```yaml
batching:
  max_size: 50          # Reduce from 100
  max_wait: 3s          # Reduce from 5s
  queue_size: 500       # Reduce from 1000
```

Restart:
```bash
make stop-local
make run-local
```

### Certificate Expiration

mTLS certificates expire after 1 year. Regenerate:

```bash
# Backup old certificates
cp -r deployments/certs/certs deployments/certs/certs.backup

# Generate new certificates
make certs

# Restart services
make stop-local
make run-local
```

---

## Monitoring and Maintenance

### Check Service Status

```bash
# Container status
podman ps -a

# Resource usage
podman stats

# Disk usage
podman system df
```

### View Recent Logs

```bash
# Last 100 lines
podman logs --tail 100 logl-server
podman logs --tail 100 logl-tailer

# Follow logs
podman logs -f logl-server
```

### MongoDB Collection Size

Log into MongoDB Atlas and check collection sizes:
```javascript
use logl
db.logs_wrangl_api.stats()
```

### Cleanup Old Logs

The server automatically deletes logs older than 30 days (configured via `ttl_days`).

To change retention:
```yaml
# In server.yaml
mongodb:
  ttl_days: 60  # Keep for 60 days
```

---

## Scaling Considerations

As your infrastructure grows:

1. **Multiple Tailers**: Deploy tailers on each application host pointing to the same server
2. **Server Replicas**: Run multiple server instances behind a load balancer
3. **MongoDB Sharding**: Enable sharding on MongoDB Atlas for collections > 100GB
4. **Network Optimization**: Use VPN or private networking between hosts and server

---

## Security Best Practices

- ✅ mTLS certificates rotated annually
- ✅ Run containers as non-root (already configured)
- ✅ MongoDB certificates secured with proper permissions
- ✅ Firewall rules limiting access to port 8443
- ✅ Regular security updates: `sudo apt-get update && sudo apt-get upgrade`
- ✅ Monitor access logs for suspicious activity

---

## Support and Resources

- **GitHub Repository**: https://github.com/oicur0t/logl
- **README**: See main README.md for architecture details
- **Quick Start**: deployments/QUICK_START.md
- **Server Deployment**: deployments/SERVER_DEPLOYMENT.md
- **Tailer Deployment**: deployments/TAILER_DEPLOYMENT.md

---

## Quick Command Reference

```bash
# Verify prerequisites
./deployments/verify-prerequisites.sh

# Generate certificates
make certs

# Build images
make docker-build

# Start services
make run-local

# Stop services
make stop-local

# View logs
podman logs -f logl-server
podman logs -f logl-tailer

# Container status
podman ps

# Restart single container
podman restart logl-server
podman restart logl-tailer

# Update and redeploy
git pull origin main && make docker-build && make stop-local && make run-local
```
