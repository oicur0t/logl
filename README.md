# logl - Lightweight Log Ingestion System

A production-ready, lightweight log ingestion system built in Go for collecting and storing logs from distributed applications into MongoDB.

## Overview

**logl** consists of two components:

1. **logl-tailer** - Tails log files on application hosts and sends them to the server
2. **logl-server** - Receives logs via HTTPS API and stores them in MongoDB

### Key Features

- **Lightweight**: Minimal resource footprint, optimized for performance
- **Secure**: mTLS authentication between tailer and server
- **Reliable**: Automatic retry with exponential backoff, crash recovery via state persistence
- **Scalable**: Batching (100 entries or 5s), connection pooling, bulk MongoDB inserts
- **Organized**: Separate MongoDB collection per service
- **Production-Ready**: Graceful shutdown, structured logging, health checks

## Architecture

```
┌─────────────────┐                    ┌─────────────────┐
│  Application    │                    │  logl-server    │
│  Host           │                    │                 │
│                 │                    │  ┌───────────┐  │
│  ┌───────────┐  │   mTLS/HTTPS      │  │  Handler  │  │
│  │ Log Files │──┼──┐                 │  └─────┬─────┘  │
│  └───────────┘  │  │                 │        │        │
│        ▲        │  │  ┌───────────┐  │  ┌─────▼─────┐  │
│        │        │  └─▶│  Tailer   │──┼─▶│  Storage  │  │
│        │        │     │           │  │  └─────┬─────┘  │
│   App Writes    │     │ ┌───────┐ │  │        │        │
│                 │     │ │Batcher│ │  │        │        │
│                 │     │ └───────┘ │  │        │        │
│                 │     └───────────┘  │        │        │
└─────────────────┘                    │        ▼        │
                                       │  ┌───────────┐  │
                                       │  │  MongoDB  │  │
                                       │  │ (X.509)   │  │
                                       │  └───────────┘  │
                                       └─────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.21+ (for building from source)
- Podman or Docker (for container deployment)
- MongoDB Atlas (with X.509 authentication)

### 1. Clone and Build

```bash
# Clone the repository
git clone https://github.com/oicur0t/logl.git
cd logl

# Initialize Go modules
make init

# Build binaries
make build

# Or build container images
make docker-build
```

### 2. Generate mTLS Certificates

```bash
make certs
```

This creates certificates in [deployments/certs/certs/](deployments/certs/certs/):
- `ca.crt` - CA certificate
- `server.crt` / `server.key` - Server certificate and key
- `client.crt` / `client.key` - Client certificate and key

### 3. Configure

Copy and edit the example configurations:

**Tailer Configuration** ([configs/tailer.example.yaml](configs/tailer.example.yaml)):
```bash
cp configs/tailer.example.yaml deployments/podman/tailer.yaml
# Edit tailer.yaml with your service name and log file paths
```

**Server Configuration** ([configs/server.example.yaml](configs/server.example.yaml)):
```bash
cp configs/server.example.yaml deployments/podman/server.yaml
# Edit server.yaml with your MongoDB URI and certificate path
```

### 4. Deploy with Podman

```bash
# Start services
make run-local

# Check status
podman ps

# View logs
podman logs -f logl-server
podman logs -f logl-tailer

# Stop services
make stop-local
```

## Configuration

### Tailer Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `service_name` | Name of the service (required) | - |
| `hostname` | Hostname (supports env vars) | System hostname |
| `log_files` | List of log files to tail | - |
| `server.url` | Server API endpoint | - |
| `batching.max_size` | Max entries per batch | 100 |
| `batching.max_wait` | Max wait time before flush | 5s |
| `mtls.*` | mTLS certificate paths | - |
| `state_file` | Path to state persistence file | `/var/lib/logl/tailer-state.json` |

See [configs/tailer.example.yaml](configs/tailer.example.yaml) for full configuration options.

### Server Configuration

| Field | Description | Default |
|-------|-------------|---------|
| `server.listen_address` | HTTP listen address | `0.0.0.0:8443` |
| `mongodb.uri` | MongoDB connection URI | - |
| `mongodb.database` | Database name | `logl` |
| `mongodb.certificate_key_file` | Path to MongoDB X.509 cert | - |
| `mongodb.ttl_days` | Auto-delete logs older than N days | 30 |
| `mtls.enabled` | Enable mTLS | `true` |

See [configs/server.example.yaml](configs/server.example.yaml) for full configuration options.

## MongoDB Setup

This project uses **MongoDB Atlas** with **X.509 certificate authentication**.

### Collections

Logs are stored in separate collections per service:
- Collection naming: `logs_{service_name}` (sanitized, lowercase)
- Example: `logs_web_api`, `logs_payment_service`

### Indexes

Automatically created indexes:
```javascript
{ timestamp: -1 }                      // Time-based queries
{ hostname: 1, timestamp: -1 }         // Per-host queries
{ timestamp: 1, expireAfterSeconds }   // TTL index (optional)
```

### Document Schema

```json
{
  "_id": ObjectId("..."),
  "service_name": "web-api",
  "hostname": "app-server-01",
  "file_path": "/var/log/app/app.log",
  "line": "2025-12-17 10:30:15 INFO Request processed",
  "timestamp": ISODate("2025-12-17T10:30:15.000Z"),
  "line_number": 12345
}
```

## API Reference

### POST /v1/logs/ingest

Ingest a batch of log entries.

**Request:**
```json
{
  "service_name": "web-api",
  "entries": [
    {
      "service_name": "web-api",
      "hostname": "app-01",
      "file_path": "/var/log/app.log",
      "line": "INFO: Request processed",
      "timestamp": "2025-12-17T10:30:15Z",
      "line_number": 123
    }
  ]
}
```

**Response:**
```json
{
  "status": "success",
  "received": 1
}
```

### GET /v1/health

Health check endpoint.

**Response:**
```json
{
  "status": "healthy"
}
```

## Operations

### State Persistence

The tailer persists its state every 10 seconds to `/var/lib/logl/tailer-state.json`:

```json
{
  "/var/log/app/app.log": {
    "offset": 1048576,
    "inode": 987654,
    "last_read": "2025-12-17T10:30:00Z"
  }
}
```

This allows the tailer to resume from the last position after a crash or restart.

### Log Rotation

The tailer automatically detects log rotation using the `tail` library:
- Handles rename-based rotation (e.g., `app.log` → `app.log.1`)
- Handles truncate-based rotation
- Seamlessly switches to new file

### Graceful Shutdown

Both components support graceful shutdown (30-second timeout):
- **Tailer**: Flushes pending batches, saves state, closes file handles
- **Server**: Completes in-flight requests, closes MongoDB connection

Trigger with `SIGTERM` or `SIGINT`:
```bash
kill -SIGTERM <pid>
# or
Ctrl+C
```

### Monitoring

Check logs for operational metrics:
```bash
# Tailer
podman logs logl-tailer | grep -E "(Batch sent|State saved)"

# Server
podman logs logl-server | grep -E "(Batch inserted|HTTP request)"
```

## Development

### Project Structure

```
logl/
├── cmd/                    # Entry points
│   ├── logl-tailer/       # Tailer binary
│   └── logl-server/       # Server binary
├── internal/              # Private application code
│   ├── tailer/           # Tailer logic
│   ├── server/           # Server logic
│   └── config/           # Configuration loading
├── pkg/                   # Public reusable packages
│   ├── models/           # Data models
│   ├── mtls/             # mTLS utilities
│   └── retry/            # Retry logic
├── configs/               # Example configs
├── deployments/           # Deployment files
│   ├── podman/           # Podman/Docker files
│   └── certs/            # Certificate generation
└── Makefile              # Build automation
```

### Building

```bash
# Build both binaries
make build

# Build individual components
make build-tailer
make build-server

# Run tests
make test

# Run linter
make lint
```

### Testing Locally

1. Generate certificates:
   ```bash
   make certs
   ```

2. Create test log file:
   ```bash
   mkdir -p /tmp/testlogs
   echo "Test log entry" >> /tmp/testlogs/app.log
   ```

3. Configure tailer to watch `/tmp/testlogs/app.log`

4. Start server and tailer:
   ```bash
   ./bin/logl-server --config deployments/podman/server.yaml
   ./bin/logl-tailer --config deployments/podman/tailer.yaml
   ```

5. Add more log entries and watch them flow to MongoDB

## Performance

### Benchmarks

- **Tailer**: 10,000 lines/sec per file
- **Server**: 100,000 lines/sec aggregate
- **MongoDB**: 50,000 inserts/sec (bulk operations)

### Optimization Tips

1. **Batching**: Tune `batching.max_size` and `batching.max_wait` for your workload
2. **Connection Pooling**: Increase `mongodb.max_pool_size` for high throughput
3. **Log Rotation**: Avoid very frequent rotation (< 1 minute)
4. **Network**: Ensure low latency between tailer and server

## Security

### mTLS Best Practices

- Use 4096-bit RSA keys
- Rotate certificates annually
- Keep private keys secure (`.gitignore` them)
- Use TLS 1.3 minimum

### MongoDB Security

- Use X.509 authentication
- Enable TLS for MongoDB connections
- Rotate MongoDB certificates regularly
- Use network policies to restrict access

## Troubleshooting

### Tailer Issues

**Tailer not reading logs:**
- Check file paths in config
- Verify file permissions (tailer needs read access)
- Check state file for errors

**High memory usage:**
- Reduce `batching.queue_size`
- Check for log file growth rate

### Server Issues

**Connection refused:**
- Verify server is running: `podman ps`
- Check mTLS certificates are valid
- Verify server URL in tailer config

**MongoDB connection failed:**
- Verify MongoDB URI and certificate path
- Check network connectivity
- Verify X.509 cert is valid

### Common Errors

**"Certificate verify failed":**
- Ensure CA cert matches server/client certs
- Check certificate expiration dates

**"Context deadline exceeded":**
- Increase timeouts in config
- Check network latency

## Contributing

Contributions are welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Run `make test` and `make lint`
5. Submit a pull request

## License

MIT License - see LICENSE file for details

## Support

For issues or questions:
- GitHub Issues: https://github.com/oicur0t/logl/issues
- Documentation: https://github.com/oicur0t/logl/wiki

## Roadmap

- [ ] Prometheus metrics endpoint
- [ ] Compression support (gzip)
- [ ] Multi-line log parsing
- [ ] Kubernetes DaemonSet deployment
- [ ] Dashboard for log visualization
- [ ] Alerting based on log patterns
