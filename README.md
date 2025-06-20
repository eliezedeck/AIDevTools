# AIDevTools

MCP servers and tools for AI-powered development.

## Components

### 🚀 Sidekick
Process management, agent communication, and notification MCP server for AI agents.

**Features:**
- Spawn and manage long-running processes
- Real-time output streaming with ring buffers
- Interactive process control (stdin/stdout/stderr)
- Audio notifications (macOS)
- Agent Q&A system for specialist communication
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

# SSE server mode with custom port
sidekick --port 6060

# Add to Claude Desktop (stdio mode)
claude mcp add sidekick ~/.local/bin/sidekick --args "--sse=false"
```

### 🌉 stdio2sse
Proxy between stdio-based MCP clients and SSE-based MCP servers.

**Features:**
- Connect Claude Desktop to SSE-only MCP servers
- Automatic tool discovery and proxying
- Transparent request/response forwarding
- Async architecture for reliable communication

**Install:**
```bash
curl -sSL https://raw.githubusercontent.com/eliezedeck/AIDevTools/main/stdio2sse/install.sh | bash
```

**Usage:**
```bash
# Bridge to an SSE server
stdio2sse --sse-url http://localhost:5050/sse

# Add to Claude Desktop
claude mcp add my-sse-server ~/.local/bin/stdio2sse --args "--sse-url" "http://localhost:5050/sse"
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
- `spawn_process` - Start a new process with options (delay, buffer size, environment)
- `spawn_multiple_processes` - Launch multiple processes sequentially
- `get_partial_process_output` - Get incremental output (tail -f functionality)
- `get_full_process_output` - Get all output in memory
- `send_process_input` - Send stdin input to a running process
- `list_processes` - List all tracked processes and their status
- `kill_process` - Terminate a tracked process
- `get_process_status` - Get detailed process information

**Agent Communication:**
- `get_next_question` - Register as a specialist and wait for questions
- `answer_question` - Provide an answer to a received question
- `ask_specialist` - Ask a question to a specialist agent
- `get_answer` - Retrieve answer for a previously asked question
- `list_specialists` - List all available specialist agents

**Notifications (macOS only for now):**
- `notifications_speak` - Play sound and speak text (max 50 words)

## License

MIT License - see [LICENSE](LICENSE) file for details.