# Sidekick Daemon

A Golang daemon that provides an MCP (Model Context Protocol) server for audio notifications and advanced process management functionality, designed for use with Claude Code and other LLMs supporting MCP.

## 🚀 Features

- MCP server using [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- **Audio Notifications**: System sound and text-to-speech (macOS only)
- **Process Management**: Spawn, monitor, and manage long-running processes with incremental output tracking
- **Real-time Output**: "tail -f" like functionality for process stdout/stderr
- **Automatic Cleanup**: Background cleanup of inactive processes (1-hour timeout)
- **Thread-safe**: Concurrent process management with proper synchronization

## 🛠️ Usage

This daemon exposes 6 MCP tools: 1 for audio notifications and 5 for process management.

### 🧩 Claude Code Integration (Official CLI Method)

1. **Add the server to Claude Code using the CLI:**
   - From the project root directory, run:

     ```bash
     claude mcp add sidekick go run "$(pwd)/sidekick/main.go" "$(pwd)/sidekick/processes.go" "$(pwd)/sidekick/notifications.go"
     ```
     - `$(pwd)` automatically uses your current directory path
     - `sidekick` is the name for this server in Claude Code

   - To add with environment variables:

     ```bash
     claude mcp add sidekick -e KEY=value go run "$(pwd)/sidekick/main.go" "$(pwd)/sidekick/processes.go" "$(pwd)/sidekick/notifications.go"
     ```

3. **Verify the server is added:**
   - Run:
     ```bash
     claude mcp list
     ```
   - You should see `sidekick` in the list.

4. **Use the tools in Claude Code:**
   - Open Claude Code in your project.
   - All 6 tools will be auto-discovered and available in the tool palette or via `/` commands.
   - You can now call the tools from chat, automations, or workflows.

---

#### 📝 Tips & Notes
- No need to edit any JSON config files manually. The CLI handles everything.
- The server will be started by Claude Code as a subprocess using stdio transport.
- Remove the server with:
  ```bash
  claude mcp remove sidekick
  ```
- For advanced options (scopes, env vars), see: [Claude Code MCP CLI Docs](https://docs.anthropic.com/en/docs/claude-code/tutorials#set-up-model-context-protocol-mcp)

---

## 🛠️ Available Tools

### Audio Notifications

#### `notifications_speak`
Plays system sound and speaks text using macOS TTS.

| Argument | Type   | Required | Description                  |
|----------|--------|----------|------------------------------|
| text     | string |   ✅     | Text to speak (max 50 words) |

**Example:**
```json
{
  "tool": "notifications_speak",
  "args": {
    "text": "Task completed successfully!"
  }
}
```

### Process Management

#### `spawn_process`
Spawn a new process and start tracking its output with color output disabled.

| Argument    | Type   | Required | Description                    |
|-------------|--------|----------|--------------------------------|
| command     | string |   ✅     | Command to execute             |
| args        | array  |   ❌     | Command arguments              |
| working_dir | string |   ❌     | Working directory              |
| env         | object |   ❌     | Environment variables          |

**Example:**
```json
{
  "tool": "spawn_process", 
  "args": {
    "command": "npm",
    "args": ["run", "dev"],
    "working_dir": "/path/to/project"
  }
}
```

#### `get_process_output`
Get incremental output from a process since last read (tail -f functionality).

| Argument   | Type    | Required | Description                           |
|------------|---------|----------|---------------------------------------|
| process_id | string  |   ✅     | Process identifier                    |
| streams    | string  |   ❌     | "stdout", "stderr", or "both" (default) |
| max_lines  | number  |   ❌     | Maximum lines to return               |

**Example:**
```json
{
  "tool": "get_process_output",
  "args": {
    "process_id": "abc123",
    "streams": "both",
    "max_lines": 50
  }
}
```

#### `list_processes`
List all tracked processes and their current status.

No arguments required.

**Example:**
```json
{
  "tool": "list_processes",
  "args": {}
}
```

#### `kill_process`
Terminate a tracked process.

| Argument   | Type   | Required | Description        |
|------------|--------|----------|--------------------|
| process_id | string |   ✅     | Process identifier |

**Example:**
```json
{
  "tool": "kill_process",
  "args": {
    "process_id": "abc123"
  }
}
```

#### `get_process_status`
Get detailed status information about a process.

| Argument   | Type   | Required | Description        |
|------------|--------|----------|--------------------|
| process_id | string |   ✅     | Process identifier |

**Example:**
```json
{
  "tool": "get_process_status",
  "args": {
    "process_id": "abc123"
  }
}
```

## ⚙️ Technical Details

### Audio Notifications
- Uses [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) for MCP server implementation
- Plays `/System/Library/Sounds/Glass.aiff` using `afplay`
- Uses `say -v "Zoe (Premium)"` for text-to-speech
- Both audio commands run concurrently
- Request returns immediately (MCP tool result) without waiting for audio completion

### Process Management
- **Thread-safe**: Uses `sync.RWMutex` for concurrent access to process registry and individual process data
- **Incremental Output**: Cursor-based reading tracks exact position in stdout/stderr buffers
- **Memory Management**: All output stored in memory with automatic cleanup after 1 hour of inactivity
- **Background Cleanup**: Goroutine runs every 15 minutes to remove stale processes
- **Color-free Output**: Spawned processes have `NO_COLOR=1` and `TERM=dumb` environment variables set
- **Process Status Tracking**: Real-time monitoring of running, completed, failed, and killed processes
- **UUID Process IDs**: Each spawned process gets a unique identifier for tracking

## 📝 Notes
- macOS only (uses `afplay` and `say` for notifications)
- Requires MCP-compatible client (e.g. Claude Code)
- Process output is kept in memory until automatic cleanup (1-hour timeout)
- Color output is automatically disabled for clean parsing

## 🧑‍💻 Development

### File Structure
- `sidekick/main.go` - MCP server setup and tool registration
- `sidekick/notifications.go` - Audio notification functionality
- `sidekick/processes.go` - Process management and output tracking

## 📚 References
- [MCP Protocol Spec](https://modelcontextprotocol.io/)
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- [Claude Code](https://claude.ai/)