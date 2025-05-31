#!/bin/bash

# Script to install sidekick as a macOS LaunchAgent service
# Run this script from the sidekick root directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_PLIST="$SCRIPT_DIR/com.sidekick.daemon.plist"
LAUNCHAGENTS_DIR="$HOME/Library/LaunchAgents"

echo "Installing sidekick service..."

# Create LaunchAgents directory if it doesn't exist
mkdir -p "$LAUNCHAGENTS_DIR"

# Copy plist file to LaunchAgents directory
cp "$SERVICE_PLIST" "$LAUNCHAGENTS_DIR/"

# Load the service
launchctl load "$LAUNCHAGENTS_DIR/$SERVICE_PLIST"

echo "Service installed and started!"
echo "The sidekick daemon will now start automatically on boot."
echo "Use 'launchctl list | grep sidekick' to verify it's running."