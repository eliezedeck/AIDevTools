#!/bin/bash

# AIDevTools Sidekick Installation Script
# Downloads and installs the latest sidekick binary from GitHub releases
# Usage: curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/install.sh | bash

set -e

# Configuration
REPO="eliezedeck/AIDevTools"
BINARY_NAME="sidekick"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
GITHUB_API="https://api.github.com/repos/$REPO"
GITHUB_RELEASES="https://github.com/$REPO/releases"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Detect OS and architecture
detect_platform() {
    local os arch
    
    case "$(uname -s)" in
        Darwin*)    os="darwin" ;;
        Linux*)     os="linux" ;;
        MINGW*|MSYS*|CYGWIN*) os="windows" ;;
        *)          log_error "Unsupported operating system: $(uname -s)"; exit 1 ;;
    esac
    
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        arm64|aarch64)  arch="arm64" ;;
        *)              log_error "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac
    
    if [[ "$os" == "windows" ]]; then
        PLATFORM="${os}-${arch}"
        BINARY_EXT=".exe"
        ARCHIVE_EXT=".zip"
    else
        PLATFORM="${os}-${arch}"
        BINARY_EXT=""
        ARCHIVE_EXT=".tar.gz"
    fi
    
    log_info "Detected platform: $PLATFORM"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Get latest release version
get_latest_version() {
    log_info "Fetching latest release information..."
    
    if command_exists curl; then
        VERSION=$(curl -s "$GITHUB_API/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    elif command_exists wget; then
        VERSION=$(wget -qO- "$GITHUB_API/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    else
        log_error "Neither curl nor wget found. Please install one of them."
        exit 1
    fi
    
    if [[ -z "$VERSION" ]]; then
        log_error "Failed to fetch latest version. Trying fallback to v0.1.0..."
        VERSION="v0.1.0"
    fi
    
    log_success "Latest version: $VERSION"
}

# Download and extract binary
download_binary() {
    local archive_name="${BINARY_NAME}-${PLATFORM}${ARCHIVE_EXT}"
    local download_url="${GITHUB_RELEASES}/download/${VERSION}/${archive_name}"
    local temp_dir
    temp_dir=$(mktemp -d)
    
    log_info "Downloading $archive_name..."
    
    if command_exists curl; then
        if ! curl -sL "$download_url" -o "$temp_dir/$archive_name"; then
            log_error "Failed to download from $download_url"
            return 1
        fi
    elif command_exists wget; then
        if ! wget -q "$download_url" -O "$temp_dir/$archive_name"; then
            log_error "Failed to download from $download_url"
            return 1
        fi
    fi
    
    log_success "Downloaded successfully"
    
    # Extract archive
    log_info "Extracting archive..."
    cd "$temp_dir"
    
    if [[ "$ARCHIVE_EXT" == ".tar.gz" ]]; then
        if ! tar -xzf "$archive_name"; then
            log_error "Failed to extract tar.gz archive"
            return 1
        fi
    elif [[ "$ARCHIVE_EXT" == ".zip" ]]; then
        if command_exists unzip; then
            if ! unzip -q "$archive_name"; then
                log_error "Failed to extract zip archive"
                return 1
            fi
        else
            log_error "unzip command not found. Please install unzip."
            return 1
        fi
    fi
    
    # Find the binary
    local binary_file="${BINARY_NAME}-${PLATFORM}${BINARY_EXT}"
    if [[ ! -f "$binary_file" ]]; then
        log_error "Binary file $binary_file not found in archive"
        return 1
    fi
    
    # Move binary to install directory
    mkdir -p "$INSTALL_DIR"
    if ! mv "$binary_file" "$INSTALL_DIR/$BINARY_NAME"; then
        log_error "Failed to move binary to $INSTALL_DIR"
        return 1
    fi
    
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    log_success "Binary installed to $INSTALL_DIR/$BINARY_NAME"
    
    # Cleanup
    rm -rf "$temp_dir"
}

# Build from source as fallback
build_from_source() {
    log_warning "Pre-built binary not available. Attempting to build from source..."
    
    if ! command_exists go; then
        log_error "Go is not installed. Please install Go from https://golang.org/dl/"
        log_error "Or wait for pre-built binaries to be available for your platform."
        exit 1
    fi
    
    if ! command_exists git; then
        log_error "Git is not installed. Please install Git."
        exit 1
    fi
    
    local temp_dir
    temp_dir=$(mktemp -d)
    
    log_info "Cloning repository..."
    if ! git clone "https://github.com/$REPO.git" "$temp_dir/AIDevTools"; then
        log_error "Failed to clone repository"
        exit 1
    fi
    
    log_info "Building from source..."
    cd "$temp_dir/AIDevTools/sidekick"
    
    if ! go mod download; then
        log_error "Failed to download Go dependencies"
        exit 1
    fi
    
    mkdir -p "$INSTALL_DIR"
    if ! go build -ldflags="-s -w" -o "$INSTALL_DIR/$BINARY_NAME" main.go processes.go notifications.go; then
        log_error "Failed to build binary"
        exit 1
    fi
    
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    log_success "Binary built and installed to $INSTALL_DIR/$BINARY_NAME"
    
    # Cleanup
    rm -rf "$temp_dir"
}

# Check if PATH includes install directory
check_path() {
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        log_warning "$INSTALL_DIR is not in your PATH"
        echo ""
        echo "Add the following line to your shell configuration file:"
        echo "  ~/.bashrc (Bash) or ~/.zshrc (Zsh) or ~/.config/fish/config.fish (Fish)"
        echo ""
        echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
        echo ""
        echo "Or run this command now:"
        echo "  echo 'export PATH=\"\$PATH:$INSTALL_DIR\"' >> ~/.$(basename "$SHELL")rc"
        echo ""
        echo "Then restart your shell or run: source ~/.$(basename "$SHELL")rc"
        echo ""
    fi
}

# Verify installation
verify_installation() {
    if [[ -x "$INSTALL_DIR/$BINARY_NAME" ]]; then
        log_success "Installation successful!"
        echo ""
        echo "📍 Binary location: $INSTALL_DIR/$BINARY_NAME"
        
        # Try to get version
        if "$INSTALL_DIR/$BINARY_NAME" --version >/dev/null 2>&1; then
            echo "🔢 Version: $("$INSTALL_DIR/$BINARY_NAME" --version 2>/dev/null || echo "unknown")"
        fi
        
        echo ""
        echo "🚀 Next steps:"
        echo "  1. Add to Claude Code:"
        echo "     claude mcp add sidekick $INSTALL_DIR/$BINARY_NAME"
        echo ""
        echo "  2. Verify MCP registration:"
        echo "     claude mcp list"
        echo ""
        echo "  3. Test in Claude Code:"
        echo "     Ask Claude to spawn a simple process!"
        echo ""
        
        check_path
        
        log_success "Ready to use with Claude Code! 🎉"
    else
        log_error "Installation failed - binary not found or not executable"
        exit 1
    fi
}

# Check for existing installation
check_existing() {
    if [[ -f "$INSTALL_DIR/$BINARY_NAME" ]]; then
        log_warning "Sidekick is already installed at $INSTALL_DIR/$BINARY_NAME"
        echo -n "Do you want to reinstall/update? [y/N]: "
        read -r response
        case "$response" in
            [yY][eE][sS]|[yY]) 
                log_info "Proceeding with reinstallation..."
                return 0
                ;;
            *)
                log_info "Installation cancelled."
                exit 0
                ;;
        esac
    fi
}

# Main installation function
main() {
    echo "🚀 AIDevTools Sidekick Installer"
    echo "=================================="
    echo ""
    
    # Check for existing installation
    check_existing
    
    # Detect platform
    detect_platform
    
    # Get latest version
    get_latest_version
    
    # Try to download binary, fallback to building from source
    if ! download_binary; then
        log_warning "Failed to download pre-built binary"
        build_from_source
    fi
    
    # Verify installation
    verify_installation
}

# Handle interruption
trap 'log_error "Installation interrupted"; exit 130' INT

# Run main function
main "$@"