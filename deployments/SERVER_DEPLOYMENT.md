# logl-server Deployment Guide

The logl-server is the centralized component that receives logs from all tailers and stores them in MongoDB.

## Deployment Options

### Option 1: Podman/Docker (Recommended for simplicity)

#### 1. Prepare Configuration

Create `server.yaml`:
```yaml
server:
  listen_address: "0.0.0.0:8443"
  read_timeout: 30s
  write_timeout: 30s
  shutdown_timeout: 30s

mongodb:
  uri: "mongodb+srv://cluster0.rrp7vpi.mongodb.net/?authSource=%24external&authMechanism=MONGODB-X509&appName=Cluster0"
  database: "logl"
  collection_prefix: "logs_"
  certificate_key_file: "/certificates/mongodb-cert.pem"
  timeout: 10s
  max_pool_size: 100
  ttl_days: 30

mtls:
  enabled: true
  ca_cert: "/etc/logl/certs/ca.crt"
  server_cert: "/etc/logl/certs/server.crt"
  server_key: "/etc/logl/certs/server.key"
  client_auth: "require"

log_level: "info"
log_format: "json"
```

#### 2. Generate Certificates

```bash
cd deployments/certs
./generate-certs.sh
```

This creates:
- `ca.crt` - CA certificate (share with tailers)
- `server.crt` / `server.key` - Server certificate

#### 3. Build and Run

```bash
# Build image
podman build -f deployments/podman/Dockerfile.server -t logl-server:latest .

# Run container
podman run -d \
  --name logl-server \
  -p 8443:8443 \
  -v $(pwd)/server.yaml:/etc/logl/server.yaml:ro \
  -v $(pwd)/deployments/certs/certs:/etc/logl/certs:ro \
  -v /certificates:/certificates:ro \
  --restart unless-stopped \
  logl-server:latest
```

#### 4. Verify

```bash
# Check logs
podman logs -f logl-server

# Health check
curl -k https://localhost:8443/v1/health
```

### Option 2: Systemd Service (Bare Metal)

#### 1. Build Binary

```bash
make build-server
sudo cp bin/logl-server /usr/local/bin/
```

#### 2. Create Systemd Service

Create `/etc/systemd/system/logl-server.service`:
```ini
[Unit]
Description=logl Log Ingestion Server
After=network.target

[Service]
Type=simple
User=logl
Group=logl
ExecStart=/usr/local/bin/logl-server --config /etc/logl/server.yaml
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/logl

[Install]
WantedBy=multi-user.target
```

#### 3. Setup

```bash
# Create user
sudo useradd -r -s /bin/false logl

# Create directories
sudo mkdir -p /etc/logl/certs
sudo mkdir -p /var/lib/logl

# Copy config and certificates
sudo cp server.yaml /etc/logl/
sudo cp deployments/certs/certs/{ca.crt,server.crt,server.key} /etc/logl/certs/

# Set permissions
sudo chown -R logl:logl /etc/logl /var/lib/logl
sudo chmod 600 /etc/logl/certs/*.key

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable logl-server
sudo systemctl start logl-server
```

#### 4. Verify

```bash
sudo systemctl status logl-server
sudo journalctl -u logl-server -f
```

### Option 3: Kubernetes Deployment

Create `k8s-server-deployment.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: logl-server-config
data:
  server.yaml: |
    server:
      listen_address: "0.0.0.0:8443"
    mongodb:
      uri: "mongodb+srv://..."
      database: "logl"
    # ... rest of config

---
apiVersion: v1
kind: Secret
metadata:
  name: logl-server-certs
type: Opaque
data:
  ca.crt: <base64-encoded>
  server.crt: <base64-encoded>
  server.key: <base64-encoded>

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: logl-server
spec:
  replicas: 2  # For high availability
  selector:
    matchLabels:
      app: logl-server
  template:
    metadata:
      labels:
        app: logl-server
    spec:
      containers:
      - name: logl-server
        image: logl-server:latest
        ports:
        - containerPort: 8443
        volumeMounts:
        - name: config
          mountPath: /etc/logl
          readOnly: true
        - name: certs
          mountPath: /etc/logl/certs
          readOnly: true
        livenessProbe:
          httpGet:
            path: /v1/health
            port: 8443
            scheme: HTTPS
          initialDelaySeconds: 10
          periodSeconds: 30
      volumes:
      - name: config
        configMap:
          name: logl-server-config
      - name: certs
        secret:
          secretName: logl-server-certs

---
apiVersion: v1
kind: Service
metadata:
  name: logl-server
spec:
  selector:
    app: logl-server
  ports:
  - port: 8443
    targetPort: 8443
  type: LoadBalancer
```

Deploy:
```bash
kubectl apply -f k8s-server-deployment.yaml
kubectl get svc logl-server  # Get external IP
```

## Firewall Configuration

The server needs:
- **Inbound**: Port 8443 from tailer hosts
- **Outbound**: MongoDB Atlas (443)

```bash
# Example: UFW
sudo ufw allow 8443/tcp
```

## Monitoring

### Health Check Endpoint

```bash
curl -k https://server-ip:8443/v1/health
```

### Logs

Monitor for:
- Connection errors from tailers
- MongoDB connection issues
- Certificate expiration warnings

```bash
# Podman
podman logs logl-server | grep -E "(ERROR|WARN)"

# Systemd
journalctl -u logl-server | grep -E "(ERROR|WARN)"
```

## Scaling

### Horizontal Scaling

For high throughput, run multiple server instances behind a load balancer:

```
    Tailers
       ↓
  Load Balancer (HAProxy/nginx)
       ↓
  ┌────┴────┐
  ↓         ↓
Server-1  Server-2
  ↓         ↓
    MongoDB
```

MongoDB handles concurrent writes naturally.

### Vertical Scaling

Increase resources based on load:
- **CPU**: 2+ cores for high throughput
- **Memory**: 2GB minimum, 4GB+ recommended
- **Network**: 1Gbps+ for high log volume

## Security Checklist

- [ ] mTLS enabled with strong certificates (4096-bit RSA)
- [ ] Certificates rotated annually
- [ ] Private keys secured (600 permissions)
- [ ] MongoDB X.509 authentication configured
- [ ] Firewall rules restrict access to tailers only
- [ ] Server runs as non-root user
- [ ] TLS 1.3 enforced

## Troubleshooting

### Server won't start
```bash
# Check config syntax
logl-server --config /etc/logl/server.yaml --validate

# Check certificate validity
openssl x509 -in /etc/logl/certs/server.crt -text -noout
```

### MongoDB connection fails
```bash
# Test MongoDB connectivity
mongosh "mongodb+srv://..." --tls --tlsCertificateKeyFile /certificates/mongodb-cert.pem

# Check certificate path in config
cat /etc/logl/server.yaml | grep certificate_key_file
```

### High memory usage
- Reduce `mongodb.max_pool_size` in config
- Check for MongoDB connection leaks
- Monitor batch sizes from tailers
