# Quick Start Deployment Guide

## Overview

**logl** has two components with different deployment patterns:

| Component | Deployment | Location | Purpose |
|-----------|------------|----------|---------|
| **logl-server** | Centralized | 1 instance (or cluster) | Receives all logs, stores in MongoDB |
| **logl-tailer** | Distributed | 1 per app host | Tails local logs, ships to server |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Your Infrastructure                   │
│                                                          │
│  App Host 1          App Host 2          App Host N     │
│  ┌──────────┐       ┌──────────┐       ┌──────────┐    │
│  │ Tailer   │       │ Tailer   │       │ Tailer   │    │
│  │ (web-api)│       │ (payment)│       │ (auth)   │    │
│  └────┬─────┘       └────┬─────┘       └────┬─────┘    │
│       └──────────────────┼──────────────────┘           │
│                          │                               │
│                          ↓                               │
│                  ┌───────────────┐                      │
│                  │  logl-server  │                      │
│                  │  (Port 8443)  │                      │
│                  └───────┬───────┘                      │
└──────────────────────────┼──────────────────────────────┘
                           ↓
                   ┌───────────────┐
                   │ MongoDB Atlas │
                   │  (Cloud)      │
                   └───────────────┘
```

## Step-by-Step Deployment

### Phase 1: Setup Server (Do Once)

#### 1.1 Generate Certificates
```bash
cd deployments/certs
./generate-certs.sh
```

This creates:
- `ca.crt` - Share with all tailers
- `server.crt`, `server.key` - Keep on server
- `client.crt`, `client.key` - Share with tailers

#### 1.2 Configure Server

Edit `deployments/podman/server.yaml`:
```yaml
mongodb:
  uri: "mongodb+srv://YOUR-CLUSTER.mongodb.net/..."
  certificate_key_file: "/certificates/mongodb-cert.pem"
  database: "logl"

mtls:
  enabled: true
  ca_cert: "/etc/logl/certs/ca.crt"
  server_cert: "/etc/logl/certs/server.crt"
  server_key: "/etc/logl/certs/server.key"
```

#### 1.3 Deploy Server

**Option A: Docker/Podman**
```bash
podman build -f deployments/podman/Dockerfile.server -t logl-server .
podman run -d \
  --name logl-server \
  -p 8443:8443 \
  -v $(pwd)/deployments/podman/server.yaml:/etc/logl/server.yaml:ro \
  -v $(pwd)/deployments/certs/certs:/etc/logl/certs:ro \
  -v /certificates:/certificates:ro \
  logl-server
```

**Option B: Systemd** (see [SERVER_DEPLOYMENT.md](SERVER_DEPLOYMENT.md))

#### 1.4 Verify Server
```bash
curl -k https://server-ip:8443/v1/health
# Should return: {"status":"healthy"}
```

### Phase 2: Deploy Tailers (Repeat Per Host)

#### 2.1 Prepare Tailer Config

Create `tailer.yaml` for each host:
```yaml
service_name: "web-api"  # ← CHANGE THIS per service

log_files:
  - path: "/var/log/app/application.log"  # ← YOUR LOG PATHS
    enabled: true

server:
  url: "https://SERVER-IP:8443/v1/logs/ingest"  # ← YOUR SERVER

mtls:
  ca_cert: "/etc/logl/certs/ca.crt"
  client_cert: "/etc/logl/certs/client.crt"
  client_key: "/etc/logl/certs/client.key"
  server_name: "logl-server"
```

#### 2.2 Copy Files to Host

```bash
# Copy binary
scp bin/logl-tailer user@app-host:/tmp/

# Copy certificates
scp deployments/certs/certs/{ca.crt,client.crt,client.key} user@app-host:/tmp/

# Copy config
scp tailer.yaml user@app-host:/tmp/
```

#### 2.3 Install on Host

```bash
# SSH to app host
ssh user@app-host

# Install
sudo mv /tmp/logl-tailer /usr/local/bin/
sudo mkdir -p /etc/logl/certs /var/lib/logl
sudo mv /tmp/{ca.crt,client.crt,client.key} /etc/logl/certs/
sudo mv /tmp/tailer.yaml /etc/logl/

# Create user and set permissions
sudo useradd -r -s /bin/false logl
sudo chown -R logl:logl /etc/logl /var/lib/logl
sudo chmod 600 /etc/logl/certs/*.key

# Add logl user to log file group
sudo usermod -a -G adm logl  # or whatever group owns your logs
```

#### 2.4 Create Systemd Service

Create `/etc/systemd/system/logl-tailer.service`:
```ini
[Unit]
Description=logl Log Tailer
After=network.target

[Service]
Type=simple
User=logl
Group=logl
ExecStart=/usr/local/bin/logl-tailer --config /etc/logl/tailer.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

#### 2.5 Start Tailer

```bash
sudo systemctl daemon-reload
sudo systemctl enable logl-tailer
sudo systemctl start logl-tailer
sudo systemctl status logl-tailer
```

### Phase 3: Verify End-to-End

#### 3.1 Generate Test Logs

On app host:
```bash
echo "Test log entry at $(date)" | sudo tee -a /var/log/app/application.log
```

#### 3.2 Check Tailer Logs

```bash
sudo journalctl -u logl-tailer -f
# Look for: "Batch sent successfully"
```

#### 3.3 Verify in MongoDB

```bash
# Connect to MongoDB
mongosh "mongodb+srv://..." --tls --tlsCertificateKeyFile /path/to/cert.pem

# Check collections
use logl
show collections
# Should see: logs_web_api (or your service name)

# Query logs
db.logs_web_api.find().sort({timestamp: -1}).limit(5)
```

## Common Deployment Scenarios

### Scenario 1: Single Application Server

```
1 tailer on app server → 1 server instance → MongoDB
```

Simplest setup. Both can run on the same host for testing:
```bash
make run-local  # Uses docker-compose
```

### Scenario 2: Multiple Application Servers

```
3 tailers (web, api, db) → 1 server instance → MongoDB
```

1. Deploy server once (central host)
2. Deploy tailer on each app host with different `service_name`
3. Each creates its own MongoDB collection

### Scenario 3: Microservices (10+ services)

```
10+ tailers → Load balanced server cluster → MongoDB
```

- Server: 2-3 instances behind load balancer
- Tailers: One per service host
- MongoDB: Sharded cluster for scale

### Scenario 4: Kubernetes

```
DaemonSet (tailers) → Server deployment → MongoDB
```

See [TAILER_DEPLOYMENT.md](TAILER_DEPLOYMENT.md#option-3-kubernetes-daemonset)

## Configuration Matrix

| Service Name | Host | Log Files | Collection |
|--------------|------|-----------|------------|
| `web-api` | web-01 | `/var/log/nginx/*.log` | `logs_web_api` |
| `payment-service` | payment-01 | `/var/log/app/payment.log` | `logs_payment_service` |
| `auth-service` | auth-01 | `/var/log/app/auth.log` | `logs_auth_service` |
| `database-proxy` | db-proxy-01 | `/var/log/mysql/slow.log` | `logs_database_proxy` |

## Monitoring Checklist

After deployment, verify:

- [ ] Server health: `curl -k https://server:8443/v1/health`
- [ ] Tailer running: `systemctl status logl-tailer`
- [ ] Tailers connecting: Check server logs for client connections
- [ ] Batches being sent: `journalctl -u logl-tailer | grep "Batch sent"`
- [ ] MongoDB collections exist: `show collections` in MongoDB
- [ ] Recent logs visible: Query MongoDB for latest entries

## Troubleshooting Quick Reference

| Symptom | Check | Fix |
|---------|-------|-----|
| Server won't start | Certificates | Verify paths in config |
| Tailer can't connect | Network/firewall | Allow port 8443 |
| Logs not tailing | File permissions | Add logl to log group |
| High memory usage | Queue size too large | Reduce `batching.queue_size` |
| Batches timing out | Network latency | Increase `server.timeout` |
| MongoDB connection fails | Certificate path | Check `certificate_key_file` |

## Production Checklist

Before going to production:

Server:
- [ ] TLS certificates from trusted CA (not self-signed)
- [ ] Firewall rules configured
- [ ] MongoDB authentication enabled
- [ ] Health check endpoint monitored
- [ ] Resource limits set
- [ ] Backup strategy for MongoDB

Tailers:
- [ ] Running as non-root user
- [ ] State directory writable
- [ ] Log rotation doesn't break tailing
- [ ] Monitoring alerts configured
- [ ] Client certificates distributed securely

## Need More Detail?

- **Server deployment**: See [SERVER_DEPLOYMENT.md](SERVER_DEPLOYMENT.md)
- **Tailer deployment**: See [TAILER_DEPLOYMENT.md](TAILER_DEPLOYMENT.md)
- **Architecture & API**: See main [README.md](../README.md)
