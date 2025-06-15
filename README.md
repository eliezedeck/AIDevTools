# AIDevTools

MCP servers and tools for AI-powered development.

## Components

### 🚀 Sidekick
Process management and notification MCP server for AI agents.

**Features:**
- Spawn and manage long-running processes
- Real-time output streaming with ring buffers
- Interactive process control (stdin/stdout/stderr)
- Audio notifications (macOS)
- TUI mode for visual process monitoring
- Cross-platform: Linux, macOS, Windows

**Install:**
```bash
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/sidekick/install.sh | bash
```

**Usage:**
```bash
# TUI mode (default)
sidekick

# Stdio mode for Claude Desktop
sidekick --sse=false

# Add to Claude Desktop
claude mcp add sidekick ~/.local/bin/sidekick
```

### 🌉 StdioBridge
Proxy between stdio-based MCP clients and SSE-based MCP servers.

**Features:**
- Connect Claude Desktop to SSE-only MCP servers
- Automatic tool discovery and proxying
- Transparent request/response forwarding

**Install:**
```bash
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/stdiobridge/install.sh | bash
```

**Usage:**
```bash
# Bridge to an SSE server
stdiobridge --sse-url http://localhost:5050/sse

# Add to Claude Desktop
claude mcp add my-sse-server ~/.local/bin/stdiobridge --args "--sse-url" "http://localhost:5050/sse"
```

## Requirements

- Go 1.23+ (for building from source)
- Claude Desktop or other MCP-compatible clients

## Building from Source

```bash
git clone https://github.com/eliezedeck/AIDevTools.git
cd AIDevTools

# Build sidekick
cd sidekick && go build

# Build stdiobridge
cd ../stdiobridge && go build
```

## API Reference

### Sidekick Tools

**Process Management:**
- `spawn_process` - Start a new process with options
- `spawn_multiple_processes` - Launch multiple processes
- `get_partial_process_output` - Get incremental output (tail -f)
- `get_full_process_output` - Get all output in memory
- `send_process_input` - Send stdin input
- `list_processes` - List all tracked processes
- `kill_process` - Terminate a process
- `get_process_status` - Get detailed process info

**Notifications (macOS):**
- `notifications_speak` - Play sound and speak text (max 50 words)

## License

MIT License - see [LICENSE](LICENSE) file for details.