# logl-tailer Deployment Guide

The logl-tailer runs on each application host to tail local log files and ship them to the central logl-server.

## Deployment Strategy

**One tailer per application host**, configured to watch that host's log files.

```
App Host 1 (web-api)         App Host 2 (payment-service)
┌──────────────────┐         ┌──────────────────┐
│ Application      │         │ Application      │
│   ↓ writes       │         │   ↓ writes       │
│ Log Files        │         │ Log Files        │
│   ↓ tailed by    │         │   ↓ tailed by    │
│ logl-tailer ─────┼────┐    │ logl-tailer ─────┼────┐
└──────────────────┘    │    └──────────────────┘    │
                        ↓                             ↓
                    ┌────────────────────────────────┐
                    │      logl-server               │
                    │         ↓                      │
                    │      MongoDB                   │
                    └────────────────────────────────┘
```

## Deployment Options

### Option 1: Systemd Service (Recommended for bare metal/VMs)

#### 1. Build Binary

```bash
# On build machine
make build-tailer

# Copy to target host
scp bin/logl-tailer user@app-host:/tmp/
```

#### 2. Install on Target Host

```bash
# Move binary
sudo mv /tmp/logl-tailer /usr/local/bin/
sudo chmod +x /usr/local/bin/logl-tailer

# Create user
sudo useradd -r -s /bin/false logl

# Create directories
sudo mkdir -p /etc/logl/certs
sudo mkdir -p /var/lib/logl
```

#### 3. Configure for This Host

Create `/etc/logl/tailer.yaml`:
```yaml
# Unique service name for this host
service_name: "web-api"  # Change per service
hostname: "${HOSTNAME}"

# Log files on THIS host
log_files:
  - path: "/var/log/app/application.log"
    enabled: true
  - path: "/var/log/app/error.log"
    enabled: true

# Central server address
server:
  url: "https://logl-server.example.com:8443/v1/logs/ingest"
  timeout: 30s
  max_retries: 5

batching:
  max_size: 100
  max_wait: 5s
  queue_size: 1000

mtls:
  ca_cert: "/etc/logl/certs/ca.crt"
  client_cert: "/etc/logl/certs/client.crt"
  client_key: "/etc/logl/certs/client.key"
  server_name: "logl-server.example.com"

state_file: "/var/lib/logl/tailer-state.json"
log_level: "info"
log_format: "json"
```

#### 4. Setup Certificates

```bash
# Copy certificates from server
scp user@cert-server:/path/to/ca.crt /etc/logl/certs/
scp user@cert-server:/path/to/client.crt /etc/logl/certs/
scp user@cert-server:/path/to/client.key /etc/logl/certs/

# Set permissions
sudo chown -R logl:logl /etc/logl /var/lib/logl
sudo chmod 600 /etc/logl/certs/*.key
```

#### 5. Add logl User to Log Group

The tailer needs read access to log files:
```bash
# Find the group that owns the log files
ls -la /var/log/app/

# Add logl user to that group
sudo usermod -a -G <log-group> logl
```

#### 6. Create Systemd Service

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

# Security
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

#### 7. Start Service

```bash
sudo systemctl daemon-reload
sudo systemctl enable logl-tailer
sudo systemctl start logl-tailer

# Verify
sudo systemctl status logl-tailer
sudo journalctl -u logl-tailer -f
```

### Option 2: Podman Container

Useful when running on container hosts.

#### 1. Build Image

```bash
podman build -f deployments/podman/Dockerfile.tailer -t logl-tailer:latest .
```

#### 2. Run Container

```bash
podman run -d \
  --name logl-tailer \
  -e HOSTNAME=$(hostname) \
  -v $(pwd)/tailer.yaml:/etc/logl/tailer.yaml:ro \
  -v $(pwd)/deployments/certs/certs:/etc/logl/certs:ro \
  -v /var/log:/var/log:ro \
  -v logl-state:/var/lib/logl \
  --restart unless-stopped \
  logl-tailer:latest
```

#### 3. Setup as Podman Systemd Service

```bash
# Generate systemd unit
podman generate systemd --new --files --name logl-tailer

# Move to systemd directory
sudo mv container-logl-tailer.service /etc/systemd/system/

# Enable
sudo systemctl daemon-reload
sudo systemctl enable container-logl-tailer
```

### Option 3: Kubernetes DaemonSet

For Kubernetes clusters, run as a DaemonSet to automatically deploy on all nodes.

Create `k8s-tailer-daemonset.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: logl-tailer-config
data:
  tailer.yaml: |
    service_name: "k8s-app"  # Override via env var
    hostname: "${HOSTNAME}"
    log_files:
      - path: "/var/log/containers/*.log"
        enabled: true
    server:
      url: "https://logl-server.logl.svc.cluster.local:8443/v1/logs/ingest"
    # ... rest of config

---
apiVersion: v1
kind: Secret
metadata:
  name: logl-tailer-certs
type: Opaque
data:
  ca.crt: <base64-encoded>
  client.crt: <base64-encoded>
  client.key: <base64-encoded>

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: logl-tailer
spec:
  selector:
    matchLabels:
      app: logl-tailer
  template:
    metadata:
      labels:
        app: logl-tailer
    spec:
      containers:
      - name: logl-tailer
        image: logl-tailer:latest
        env:
        - name: HOSTNAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: config
          mountPath: /etc/logl
          readOnly: true
        - name: certs
          mountPath: /etc/logl/certs
          readOnly: true
        - name: logs
          mountPath: /var/log
          readOnly: true
        - name: state
          mountPath: /var/lib/logl
      volumes:
      - name: config
        configMap:
          name: logl-tailer-config
      - name: certs
        secret:
          secretName: logl-tailer-certs
      - name: logs
        hostPath:
          path: /var/log
      - name: state
        hostPath:
          path: /var/lib/logl
          type: DirectoryOrCreate
```

Deploy:
```bash
kubectl apply -f k8s-tailer-daemonset.yaml
kubectl get daemonset logl-tailer
kubectl logs -l app=logl-tailer -f
```

## Configuration Per Service

Each application service should have its own `service_name` in the tailer config:

### Web API Host
```yaml
service_name: "web-api"
log_files:
  - path: "/var/log/nginx/access.log"
  - path: "/var/log/app/web-api.log"
```

### Payment Service Host
```yaml
service_name: "payment-service"
log_files:
  - path: "/var/log/app/payment.log"
  - path: "/var/log/app/transactions.log"
```

This ensures logs go into separate MongoDB collections: `logs_web_api` and `logs_payment_service`.

## Automated Deployment with Ansible

Example playbook for deploying to multiple hosts:

```yaml
# deploy-tailer.yml
---
- name: Deploy logl-tailer
  hosts: app_servers
  become: yes
  vars:
    logl_server_url: "https://logl-server.example.com:8443"

  tasks:
    - name: Copy binary
      copy:
        src: "{{ playbook_dir }}/../bin/logl-tailer"
        dest: /usr/local/bin/logl-tailer
        mode: '0755'

    - name: Create logl user
      user:
        name: logl
        system: yes
        shell: /bin/false

    - name: Create directories
      file:
        path: "{{ item }}"
        state: directory
        owner: logl
        group: logl
      loop:
        - /etc/logl/certs
        - /var/lib/logl

    - name: Deploy config
      template:
        src: tailer.yaml.j2
        dest: /etc/logl/tailer.yaml
        owner: logl
        group: logl

    - name: Copy certificates
      copy:
        src: "{{ item }}"
        dest: "/etc/logl/certs/"
        owner: logl
        group: logl
        mode: '0600'
      loop:
        - certs/ca.crt
        - certs/client.crt
        - certs/client.key

    - name: Add logl to log group
      user:
        name: logl
        groups: adm
        append: yes

    - name: Deploy systemd service
      copy:
        src: logl-tailer.service
        dest: /etc/systemd/system/

    - name: Enable and start service
      systemd:
        name: logl-tailer
        enabled: yes
        state: started
        daemon_reload: yes
```

Run:
```bash
ansible-playbook -i inventory deploy-tailer.yml
```

## Monitoring

### Check Tailer Status

```bash
# Systemd
sudo systemctl status logl-tailer
sudo journalctl -u logl-tailer -f

# Podman
podman logs -f logl-tailer
```

### Verify Log Shipping

```bash
# Check state file (shows last read positions)
sudo cat /var/lib/logl/tailer-state.json

# Check for errors
sudo journalctl -u logl-tailer | grep ERROR

# Monitor batch sends
sudo journalctl -u logl-tailer | grep "Batch sent"
```

### Common Log Messages

```
✅ "State loaded" - Resumed from last position
✅ "Batch sent successfully" - Logs delivered to server
✅ "State saved" - Progress persisted
⚠️  "Timeout sending line to batcher" - Queue full (increase queue_size)
❌ "Circuit breaker is open" - Server unreachable
```

## Troubleshooting

### Tailer won't start

```bash
# Check config syntax
logl-tailer --config /etc/logl/tailer.yaml --validate

# Check file permissions
ls -la /var/log/app/
sudo -u logl cat /var/log/app/application.log
```

### Can't connect to server

```bash
# Test TLS connectivity
openssl s_client -connect logl-server.example.com:8443 \
  -CAfile /etc/logl/certs/ca.crt \
  -cert /etc/logl/certs/client.crt \
  -key /etc/logl/certs/client.key

# Check DNS
nslookup logl-server.example.com

# Check firewall
telnet logl-server.example.com 8443
```

### Logs not being tailed

```bash
# Verify file exists
ls -la /var/log/app/application.log

# Check if file is growing
watch -n 1 'wc -l /var/log/app/application.log'

# Verify logl user can read
sudo -u logl cat /var/log/app/application.log

# Check state file for errors
sudo cat /var/lib/logl/tailer-state.json | jq .
```

## Security Best Practices

- [ ] Run as dedicated `logl` user (not root)
- [ ] Client certificates unique per host (optional but recommended)
- [ ] Private keys protected (600 permissions)
- [ ] Only read access to log files needed
- [ ] State file directory writable only by logl user
- [ ] TLS 1.3 enforced for server communication

## Performance Tuning

### High Log Volume

If logs generate faster than tailer can send:

```yaml
batching:
  max_size: 200        # Increase batch size
  max_wait: 3s         # Reduce wait time
  queue_size: 5000     # Increase queue
```

### Multiple Fast-Growing Files

Ensure enough system resources:
- **Memory**: ~50MB per tailer + (queue_size × 1KB)
- **CPU**: Minimal (usually <5%)
- **Disk I/O**: Read-heavy workload

### Reduce Network Traffic

```yaml
# Future enhancement: compression
# Coming in v2.0
```

## Upgrading

```bash
# Stop service
sudo systemctl stop logl-tailer

# Backup state
sudo cp /var/lib/logl/tailer-state.json /var/lib/logl/tailer-state.json.backup

# Replace binary
sudo cp bin/logl-tailer /usr/local/bin/

# Start service
sudo systemctl start logl-tailer

# Verify
sudo systemctl status logl-tailer
```

State file compatibility is maintained across versions.
