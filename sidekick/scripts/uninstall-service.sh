#!/bin/bash

# Script to uninstall sidekick macOS LaunchAgent service

SERVICE_PLIST="com.sidekick.daemon.plist"
LAUNCHAGENTS_DIR="$HOME/Library/LaunchAgents"

echo "Uninstalling sidekick service..."

# Unload the service using bootout (modern macOS)
launchctl bootout gui/$(id -u)/com.sidekick.daemon 2>/dev/null

# Remove plist file from LaunchAgents directory
rm -f "$LAUNCHAGENTS_DIR/$SERVICE_PLIST"

echo "Service uninstalled!"
echo "The sidekick daemon will no longer start automatically on boot."