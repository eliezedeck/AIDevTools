#!/bin/bash

# Sidekick Installation Script
# Builds and installs the sidekick binary to ~/.local/bin/

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SIDEKICK_DIR="$SCRIPT_DIR/sidekick"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="sidekick"
INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME"

echo "🚀 Sidekick Installation Script"
echo "==============================="

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Error: Go is not installed or not in PATH"
    echo "   Please install Go from https://golang.org/dl/"
    exit 1
fi

echo "✅ Go found: $(go version)"

# Check if sidekick directory exists
if [ ! -d "$SIDEKICK_DIR" ]; then
    echo "❌ Error: sidekick directory not found at $SIDEKICK_DIR"
    exit 1
fi

# Create ~/.local/bin if it doesn't exist
if [ ! -d "$INSTALL_DIR" ]; then
    echo "📁 Creating directory: $INSTALL_DIR"
    mkdir -p "$INSTALL_DIR"
fi

# Check if binary already exists
if [ -f "$INSTALL_PATH" ]; then
    echo "⚠️  Sidekick binary already exists at: $INSTALL_PATH"
    echo -n "   Do you want to overwrite it? [y/N]: "
    read -r response
    case "$response" in
        [yY][eE][sS]|[yY]) 
            echo "   Proceeding with overwrite..."
            ;;
        *)
            echo "   Installation cancelled."
            exit 0
            ;;
    esac
fi

# Download dependencies and build the binary
echo "📦 Downloading Go dependencies..."
cd "$SIDEKICK_DIR"

# Download and verify dependencies
go mod download
if [ $? -ne 0 ]; then
    echo "❌ Failed to download dependencies!"
    exit 1
fi

echo "✅ Dependencies downloaded successfully!"

echo "🔨 Building sidekick binary..."
# Build with all Go files
go build -o "$INSTALL_PATH" main.go processes.go notifications.go

if [ $? -eq 0 ]; then
    echo "✅ Binary built successfully!"
else
    echo "❌ Build failed!"
    exit 1
fi

# Make binary executable
chmod +x "$INSTALL_PATH"

# Check if ~/.local/bin is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "⚠️  Warning: $INSTALL_DIR is not in your PATH"
    echo "   Add the following line to your ~/.bashrc, ~/.zshrc, or equivalent:"
    echo "   export PATH=\"\$PATH:$INSTALL_DIR\""
    echo ""
    echo "   Or run this command now:"
    echo "   echo 'export PATH=\"\$PATH:$INSTALL_DIR\"' >> ~/.$(basename $SHELL)rc"
    echo ""
fi

echo "🎉 Installation complete!"
echo "   Binary installed at: $INSTALL_PATH"
echo "   You can now use: claude mcp add sidekick $INSTALL_PATH"
echo ""
echo "🔍 Verify installation:"
echo "   $INSTALL_PATH --help"