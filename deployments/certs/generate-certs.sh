#!/bin/bash
set -e

CERTS_DIR="$(dirname "$0")/certs"
mkdir -p "$CERTS_DIR"

# Check if certificates already exist
if [ -f "$CERTS_DIR/ca.crt" ] && [ -f "$CERTS_DIR/server.crt" ] && [ -f "$CERTS_DIR/client.crt" ]; then
  echo "Certificates already exist in $CERTS_DIR"
  echo "Skipping generation. To regenerate, delete existing certificates first."
  exit 0
fi

echo "Generating mTLS certificates for logl..."

# Generate CA
echo "1. Generating CA certificate..."
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
  -keyout "$CERTS_DIR/ca.key" \
  -out "$CERTS_DIR/ca.crt" \
  -subj "/CN=logl-ca/O=logl" \
  2>/dev/null

# Generate Server Certificate
echo "2. Generating server certificate..."
openssl req -newkey rsa:4096 -nodes \
  -keyout "$CERTS_DIR/server.key" \
  -out "$CERTS_DIR/server.csr" \
  -subj "/CN=logl-server/O=logl" \
  2>/dev/null

openssl x509 -req -in "$CERTS_DIR/server.csr" \
  -CA "$CERTS_DIR/ca.crt" \
  -CAkey "$CERTS_DIR/ca.key" \
  -CAcreateserial -out "$CERTS_DIR/server.crt" \
  -days 365 \
  -extfile <(printf "subjectAltName=DNS:logl-server,DNS:localhost,IP:127.0.0.1") \
  2>/dev/null

# Generate Client Certificate
echo "3. Generating client certificate..."
openssl req -newkey rsa:4096 -nodes \
  -keyout "$CERTS_DIR/client.key" \
  -out "$CERTS_DIR/client.csr" \
  -subj "/CN=logl-tailer/O=logl" \
  2>/dev/null

openssl x509 -req -in "$CERTS_DIR/client.csr" \
  -CA "$CERTS_DIR/ca.crt" \
  -CAkey "$CERTS_DIR/ca.key" \
  -CAcreateserial -out "$CERTS_DIR/client.crt" \
  -days 365 \
  2>/dev/null

# Clean up CSR files
rm -f "$CERTS_DIR"/*.csr

# Set permissions (644 for container access - files mounted read-only)
chmod 644 "$CERTS_DIR"/*.crt
chmod 644 "$CERTS_DIR"/*.key

echo ""
echo "✓ Certificates generated successfully in $CERTS_DIR"
echo ""
echo "Files created:"
echo "  - ca.crt (CA certificate)"
echo "  - ca.key (CA private key)"
echo "  - server.crt (Server certificate)"
echo "  - server.key (Server private key)"
echo "  - client.crt (Client certificate)"
echo "  - client.key (Client private key)"
echo ""
echo "Note: Keep private keys (.key files) secure and never commit them to version control!"
