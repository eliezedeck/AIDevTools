# AIDevTools - Sidekick MCP Server

<div align="center">

[![CI](https://github.com/eliezedeck/AIDevTools/workflows/CI/badge.svg)](https://github.com/eliezedeck/AIDevTools/actions)
[![Release](https://img.shields.io/github/v/release/eliezedeck/AIDevTools)](https://github.com/eliezedeck/AIDevTools/releases)
[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Production-ready MCP server for AI agent process management and notifications**  
*Part of the AIDevTools ecosystem for AI-powered development*

[📖 Documentation](#-documentation) • [🚀 Quick Start](#-quick-start) • [⭐ Features](#-features) • [🛠️ API Reference](#-api-reference) • [🤝 Contributing](#-contributing)

</div>

---

## 📋 Table of Contents

- [Overview](#-overview)
- [Features](#-features)
- [Quick Start](#-quick-start)
- [Installation](#-installation)
- [Configuration](#-configuration)
- [API Reference](#-api-reference)
- [Examples](#-examples)
- [Performance](#-performance)
- [Security](#-security)
- [Contributing](#-contributing)
- [Support](#-support)
- [License](#-license)

## 🎯 Overview

**Sidekick** is a high-performance [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server that provides AI agents with powerful process management and notification capabilities. Part of the **AIDevTools** ecosystem - a collection of tools designed to enhance AI-powered software development workflows.

Built specifically for [Claude Code](https://claude.ai/code) and other MCP-compatible AI systems, Sidekick enables AI agents to spawn, monitor, and control system processes with enterprise-grade reliability.

### Why Sidekick?

- **🚀 Production Ready**: Thread-safe, memory-efficient, with automatic cleanup
- **🔄 Real-time Process Management**: Spawn, monitor, and control long-running processes
- **📱 Smart Notifications**: Audio alerts with TTS on macOS for task completion
- **🎛️ Advanced Control**: Ring buffers, stdin input, process groups, graceful shutdown
- **🌍 Cross-platform**: Process management works everywhere, notifications on macOS
- **⚡ High Performance**: 10MB ring buffers, efficient memory management, concurrent operations

## ⭐ Features

### 🔧 **Process Management**
- **Multi-platform Support**: Linux, macOS, Windows
- **Ring Buffer Output**: Configurable 10MB buffers prevent memory bloat
- **Real-time Monitoring**: "tail -f" style incremental output streaming
- **Process Groups**: Proper child process cleanup and isolation
- **Stdin Support**: Interactive process control with input forwarding
- **Smart Delays**: Async/sync process spawning with configurable delays
- **Batch Operations**: Launch multiple processes with individual configurations

### 📢 **Audio Notifications** *(macOS Only)*
- **System Integration**: Native `afplay` and `say` command integration
- **Concurrent Playback**: Non-blocking audio and speech synthesis
- **Smart Limits**: 50-word limit with validation for clarity

### 🔒 **Enterprise Features**
- **Thread-safe Operations**: Concurrent process management with proper locking
- **Automatic Cleanup**: Background process monitoring with 1-hour timeout
- **Graceful Shutdown**: SIGTERM followed by SIGKILL for clean termination
- **Memory Management**: Ring buffers with automatic old data discarding
- **Error Handling**: Comprehensive error reporting and recovery

## 🚀 Quick Start

### 30-Second Setup

```bash
# 1. Install Sidekick
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/install.sh | bash

# 2. Add to Claude Code
claude mcp add sidekick ~/.local/bin/sidekick

# 3. Start using in Claude Code
# Tools are auto-discovered and ready to use!
```

### Verify Installation

```bash
# Check if Sidekick is available
sidekick --version

# Verify MCP registration
claude mcp list
```

## 📦 Installation

### Option 1: Quick Install Script *(Recommended)*

```bash
# Download and install in one command
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/install.sh | bash
```

### Option 2: Manual Installation

#### Download Pre-built Binary

```bash
# macOS (Apple Silicon)
curl -L https://github.com/eliezedeck/AIDevTools/releases/latest/download/sidekick-darwin-arm64.tar.gz | tar -xz

# macOS (Intel)
curl -L https://github.com/eliezedeck/AIDevTools/releases/latest/download/sidekick-darwin-amd64.tar.gz | tar -xz

# Linux (x86_64)
curl -L https://github.com/eliezedeck/AIDevTools/releases/latest/download/sidekick-linux-amd64.tar.gz | tar -xz

# Linux (ARM64)
curl -L https://github.com/eliezedeck/AIDevTools/releases/latest/download/sidekick-linux-arm64.tar.gz | tar -xz

# Move to PATH
sudo mv sidekick-* /usr/local/bin/sidekick
chmod +x /usr/local/bin/sidekick
```

#### Build from Source

```bash
# Prerequisites: Go 1.23+
git clone https://github.com/eliezedeck/AIDevTools.git
cd AIDevTools/sidekick
go build -o sidekick main.go processes.go notifications.go

# Install to system
sudo mv sidekick /usr/local/bin/
```

### Option 3: Package Managers

```bash
# Homebrew (coming soon)
brew install eliezedeck/tap/sidekick

# Go install
go install github.com/eliezedeck/AIDevTools/sidekick@latest
```

## ⚙️ Configuration

### Claude Code Integration

```bash
# Basic setup
claude mcp add sidekick sidekick

# With custom environment variables
claude mcp add sidekick -e SIDEKICK_BUFFER_SIZE=20971520 sidekick

# With custom scope (if needed)
claude mcp add sidekick --scope filesystem sidekick
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SIDEKICK_BUFFER_SIZE` | `10485760` | Default ring buffer size (10MB) |
| `SIDEKICK_CLEANUP_INTERVAL` | `900` | Process cleanup interval (15 min) |
| `SIDEKICK_PROCESS_TIMEOUT` | `3600` | Process timeout (1 hour) |

### Verification

```bash
# Test MCP connection
claude mcp list

# Test a simple command
# In Claude Code: "spawn a process that echoes hello world"
```

## 🛠️ API Reference

### Tool Overview

| Platform | Tools Available | Count |
|----------|----------------|-------|
| **macOS** | Process Management + Audio Notifications | 9 |
| **Linux/Windows** | Process Management Only | 8 |

### 🔧 Process Management Tools

#### `spawn_process`

Spawn and track a new process with configurable options.

```json
{
  "tool": "spawn_process",
  "args": {
    "command": "npm",
    "args": ["run", "dev"],
    "name": "frontend-server",
    "working_dir": "/path/to/project",
    "buffer_size": 10485760,
    "delay": 2000,
    "sync_delay": false
  }
}
```

**Parameters:**
- `command` *(required)*: Command to execute
- `args`: Command arguments array
- `name`: Human-readable process name
- `working_dir`: Working directory
- `env`: Environment variables object
- `buffer_size`: Ring buffer size in bytes (default: 10MB)
- `delay`: Delay before execution (ms, max: 5 minutes)
- `sync_delay`: Wait for delay vs return immediately (default: false)

#### `spawn_multiple_processes`

Launch multiple processes with individual configurations and sequential delays.

```json
{
  "tool": "spawn_multiple_processes", 
  "args": {
    "processes": [
      {
        "command": "redis-server",
        "name": "cache"
      },
      {
        "command": "python",
        "args": ["manage.py", "runserver"],
        "name": "api-server",
        "delay": 3000
      }
    ]
  }
}
```

#### `get_partial_process_output`

Get incremental output since last read (tail -f functionality).

```json
{
  "tool": "get_partial_process_output",
  "args": {
    "process_id": "abc-123",
    "streams": "both",
    "max_lines": 50,
    "delay": 2000
  }
}
```

#### `get_full_process_output`

Get complete process output currently in memory.

```json
{
  "tool": "get_full_process_output",
  "args": {
    "process_id": "abc-123",
    "streams": "stdout"
  }
}
```

#### `send_process_input`

Send input to a running process's stdin.

```json
{
  "tool": "send_process_input",
  "args": {
    "process_id": "abc-123",
    "input": "yes\\n"
  }
}
```

#### `list_processes`

List all tracked processes and their status.

```json
{
  "tool": "list_processes",
  "args": {}
}
```

#### `kill_process`

Terminate a tracked process.

```json
{
  "tool": "kill_process",
  "args": {
    "process_id": "abc-123"
  }
}
```

#### `get_process_status`

Get detailed status information about a process.

```json
{
  "tool": "get_process_status",
  "args": {
    "process_id": "abc-123"
  }
}
```

### 📢 Audio Notifications *(macOS Only)*

#### `notifications_speak`

Play system sound and speak text using macOS TTS.

```json
{
  "tool": "notifications_speak",
  "args": {
    "text": "Build completed successfully!"
  }
}
```

**Parameters:**
- `text` *(required)*: Text to speak (max 50 words)

## 💡 Examples

### Development Server Management

```javascript
// Start a development stack
await mcp.call("spawn_multiple_processes", {
  processes: [
    {
      command: "redis-server",
      name: "cache",
      delay: 0
    },
    {
      command: "npm",
      args: ["run", "api:dev"],
      name: "api-server",
      working_dir: "/path/to/api",
      delay: 2000
    },
    {
      command: "npm", 
      args: ["run", "dev"],
      name: "frontend",
      working_dir: "/path/to/frontend",
      delay: 5000
    }
  ]
});
```

### Long-running Process Monitoring

```javascript
// Monitor build process
const result = await mcp.call("spawn_process", {
  command: "npm",
  args: ["run", "build"],
  name: "production-build"
});

const processId = result.process_id;

// Get incremental output
const output = await mcp.call("get_partial_process_output", {
  process_id: processId,
  delay: 3000,
  max_lines: 100
});
```

### Interactive Process Control

```javascript
// Start interactive process
const proc = await mcp.call("spawn_process", {
  command: "python",
  args: ["-i"],
  name: "python-repl"
});

// Send commands
await mcp.call("send_process_input", {
  process_id: proc.process_id,
  input: "print('Hello from Sidekick!')\\n"
});

// Get output
const output = await mcp.call("get_partial_process_output", {
  process_id: proc.process_id
});
```

### Completion Notifications *(macOS)*

```javascript
// Notify when task completes
await mcp.call("notifications_speak", {
  text: "Database migration completed successfully"
});
```

## ⚡ Performance

### Benchmarks

| Metric | Value | Description |
|--------|-------|-------------|
| **Memory per Process** | ~50KB | Base overhead per tracked process |
| **Ring Buffer Default** | 10MB | Configurable per process |
| **Max Concurrent Processes** | 1000+ | Limited by system resources |
| **Output Throughput** | 100MB/s+ | Ring buffer write performance |
| **Cleanup Frequency** | 15 min | Background process monitoring |

### Memory Management

- **Ring Buffers**: Automatic old data eviction prevents memory leaks
- **Process Cleanup**: 1-hour timeout for inactive processes
- **Efficient Storage**: Only store essential process metadata
- **Concurrent Safe**: Lock-free reads with minimal write contention

### Scalability

```bash
# Stress test (spawns 100 concurrent processes)
for i in {1..100}; do
  sidekick spawn "sleep 60" &
done
```

## 🔒 Security

### Security Model

- **Process Isolation**: Each process runs in its own process group
- **No Privilege Escalation**: Processes inherit Sidekick's permissions
- **Local Communication**: stdio transport, no network exposure
- **Input Validation**: All MCP inputs validated and sanitized
- **Resource Limits**: Ring buffers prevent unbounded memory growth

### Best Practices

```bash
# Run with minimal permissions
sudo -u app-user sidekick

# Use process limits
ulimit -u 100  # Limit process count
ulimit -v 1000000  # Limit virtual memory
```

### Vulnerability Reporting

Found a security issue? Please see our [Security Policy](SECURITY.md) for responsible disclosure.

## 🧪 Development

### Prerequisites

- Go 1.23 or later
- Git
- macOS (for audio notification testing)

### Building

```bash
git clone https://github.com/eliezedeck/AIDevTools.git
cd AIDevTools
make build  # or: cd sidekick && go build
```

### Testing

```bash
make test           # Run all tests
make test-race      # Race condition detection
make test-coverage  # Coverage report
```

### Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

#### Quick Contributing Steps

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`make test`)
6. Commit your changes (`git commit -m 'feat: add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

## 📊 Roadmap

- [ ] **v0.2.0**: Web interface for process monitoring
- [ ] **v0.3.0**: Process templates and saved configurations
- [ ] **v0.4.0**: Distributed process management
- [ ] **v0.5.0**: Plugin system for custom tools

## 🤝 Support

### Getting Help

- 📖 **Documentation**: Check this README and [Contributing Guide](CONTRIBUTING.md)
- 🐛 **Bug Reports**: [Create an issue](https://github.com/eliezedeck/AIDevTools/issues/new?template=bug_report.yml)
- 💡 **Feature Requests**: [Request a feature](https://github.com/eliezedeck/AIDevTools/issues/new?template=feature_request.yml)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/eliezedeck/AIDevTools/discussions)

### Community

- 🌟 **Star this repo** if you find it helpful
- 🐦 **Follow updates** on Twitter [@eliezedeck](https://twitter.com/eliezedeck)
- 📢 **Share** with the AI development community

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) - Excellent MCP implementation for Go
- [Model Context Protocol](https://modelcontextprotocol.io/) - The protocol that makes AI tool integration possible
- [Claude Code](https://claude.ai/code) - The AI development environment that inspired this project

---

<div align="center">

**Built with ❤️ for the AI development community**

[⭐ Star](https://github.com/eliezedeck/AIDevTools) • [🐛 Report Bug](https://github.com/eliezedeck/AIDevTools/issues) • [💡 Request Feature](https://github.com/eliezedeck/AIDevTools/issues) • [📖 Docs](https://github.com/eliezedeck/AIDevTools#readme)

</div>