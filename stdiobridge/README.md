# StdioBridge

Proxy between stdio-based MCP clients (like Claude Desktop) and SSE-based MCP servers.

## Architecture

```
┌─────────────┐    STDIO    ┌──────────────┐    SSE     ┌─────────────┐
│ MCP Client  │ ◄──────────► │ stdiobridge  │ ◄─────────► │ SSE Server  │
│  (Claude)   │              │   (proxy)    │             │             │
└─────────────┘              └──────────────┘             └─────────────┘
```

## Quick Start

```bash
# Install
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/stdiobridge/install.sh | bash

# Run
stdiobridge --sse-url http://localhost:5050/sse

# Add to Claude Desktop
claude mcp add my-sse-server ~/.local/bin/stdiobridge --args "--sse-url" "http://localhost:5050/sse"
```


## Options

- `--sse-url` (required): URL of the SSE MCP server
- `--name`: Bridge server name (default: "SSE Bridge")
- `--bridge-version`: Bridge version (default: "1.0.0")
- `--verbose`: Enable debug logging
- `--version`: Show version

## Building

```bash
go build -o stdiobridge
# or
make build
```

## How It Works

1. Connects to SSE server as a client
2. Discovers available tools
3. Creates stdio server with proxy handlers
4. Forwards all requests/responses transparently

## Requirements

- Go 1.23+ (for building)
- An SSE-based MCP server to connect to