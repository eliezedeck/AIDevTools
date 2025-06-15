#!/bin/bash

# Build script for stdiobridge

VERSION=${1:-dev}

echo "Building stdiobridge version $VERSION..."

# Build for current platform
go build -ldflags "-X main.version=$VERSION" -o stdiobridge .

if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo "Binary: ./stdiobridge"
    echo ""
    echo "Usage: ./stdiobridge --sse-url <URL>"
else
    echo "Build failed!"
    exit 1
fi