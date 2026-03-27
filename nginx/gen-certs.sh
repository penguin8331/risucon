#!/bin/bash

set -e

TARGET=${1:?"使い方: $0 <your-ip-address>"}
CERT_DIR="$(dirname "$0")/certs"

mkdir -p "$CERT_DIR"

SAN="IP:$TARGET"

openssl req -x509 -nodes -days 825 -newkey rsa:2048 \
    -keyout "$CERT_DIR/server.key" \
    -out    "$CERT_DIR/server.crt" \
    -subj   "/CN=$TARGET" \
    -addext "subjectAltName=$SAN"

echo "証明書を生成しました: $CERT_DIR/"
