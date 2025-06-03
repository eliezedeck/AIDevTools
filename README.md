# Sidekick Daemon

A Golang daemon that provides an MCP (Model Context Protocol) server for audio notifications and advanced process management functionality, designed for use with Claude Code and other LLMs supporting MCP.

## 🚀 Features

- MCP server using [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- **Audio Notifications**: System sound and text-to-speech (macOS only)
- **Process Management**: Spawn, monitor, and manage long-running processes with ring buffer output tracking and stdin input
- **Process Naming**: Optional human-readable names for easy process identification
- **Batch Spawning**: Launch multiple processes with individual configurations and delays
- **Real-time Output**: "tail -f" like functionality with configurable 10MB ring buffers
- **Automatic Cleanup**: Background cleanup of inactive processes (1-hour timeout)
- **Thread-safe**: Concurrent process management with proper synchronization

## 🛠️ Installation & Usage

This daemon exposes 9 MCP tools: 1 for audio notifications and 8 for process management.

### 📦 Quick Installation

1. **Install the sidekick binary:**
   - From the project root directory, run:

     ```bash
     ./install.sh
     ```
     
     This will:
     - Build the sidekick binary from all Go source files
     - Install it to `~/.local/bin/sidekick`
     - Ask for confirmation before overwriting existing installations
     - Provide PATH setup instructions if needed

### 🧩 Claude Code Integration

2. **Add the server to Claude Code:**
   - After installation, run:

     ```bash
     claude mcp add sidekick ~/.local/bin/sidekick
     ```
     
   - Or if `~/.local/bin` is in your PATH:

     ```bash
     claude mcp add sidekick sidekick
     ```

   - To add with environment variables:

     ```bash
     claude mcp add sidekick -e KEY=value ~/.local/bin/sidekick
     ```

3. **Verify the server is added:**
   - Run:
     ```bash
     claude mcp list
     ```
   - You should see `sidekick` in the list.

4. **Use the tools in Claude Code:**
   - Open Claude Code in your project.
   - All 8 tools will be auto-discovered and available in the tool palette or via `/` commands.
   - You can now call the tools from chat, automations, or workflows.

---

#### 📝 Tips & Notes
- No need to edit any JSON config files manually. The CLI handles everything.
- The binary approach is more efficient than `go run` and handles multiple source files automatically.
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
Spawn a new process and start tracking its output with configurable ring buffer size and optional delay functionality.

| Argument      | Type    | Required | Description                           |
|---------------|---------|----------|---------------------------------------|
| command       | string  |   ✅     | Command to execute                    |
| args          | array   |   ❌     | Command arguments                     |
| name          | string  |   ❌     | Optional human-readable name for the process (non-unique) |
| working_dir   | string  |   ❌     | Working directory                     |
| env           | object  |   ❌     | Environment variables                 |
| buffer_size   | number  |   ❌     | Ring buffer size in bytes (default: 10MB) |
| delay         | number  |   ❌     | Delay before execution in milliseconds (max: 5 minutes) |
| sync_delay    | boolean |   ❌     | If true, waits for delay then returns; if false, returns immediately with "pending" status (default: false) |

**Examples:**
```json
{
  "tool": "spawn_process", 
  "args": {
    "command": "npm",
    "args": ["run", "dev"],
    "working_dir": "/path/to/project",
    "buffer_size": 5242880
  }
}
```

```json
{
  "tool": "spawn_process",
  "args": {
    "command": "echo",
    "args": ["Delayed execution"],
    "delay": 3000,
    "sync_delay": false
  }
}
```

```json
{
  "tool": "spawn_process",
  "args": {
    "command": "npm",
    "args": ["run", "dev"],
    "name": "frontend-server",
    "working_dir": "/path/to/frontend"
  }
}
```

#### `spawn_multiple_processes`
Spawn multiple processes sequentially with individual configurations and delays.

| Argument   | Type    | Required | Description                                |
|------------|---------|----------|--------------------------------------------|
| processes  | array   |   ✅     | Array of process configurations to spawn  |

Each process configuration supports all parameters from `spawn_process`:
- `command` (required), `args`, `name`, `working_dir`, `env`, `buffer_size`, `delay`, `sync_delay`

**Important**: Delays are sequential - each process's delay is applied after the previous process has been scheduled, ensuring proper timing between launches.

**Example:**
```json
{
  "tool": "spawn_multiple_processes",
  "args": {
    "processes": [
      {
        "command": "redis-server",
        "name": "cache",
        "delay": 0
      },
      {
        "command": "python",
        "args": ["manage.py", "runserver"],
        "name": "backend-api",
        "working_dir": "/path/to/backend",
        "delay": 2000,
        "sync_delay": true
      },
      {
        "command": "npm",
        "args": ["start"],
        "name": "frontend-server",
        "working_dir": "/path/to/frontend",
        "delay": 3000,
        "sync_delay": false
      }
    ]
  }
}
```

Returns an array with the status of each process:
```json
[
  {"index": 0, "name": "cache", "process_id": "abc123", "pid": 12345, "status": "running"},
  {"index": 1, "name": "backend-api", "process_id": "def456", "pid": 12346, "status": "running"},
  {"index": 2, "name": "frontend-server", "process_id": "ghi789", "pid": 0, "status": "pending"}
]
```

**Timing behavior:**
- With the example above, the total execution time would be ~5 seconds:
  - t=0s: redis-server starts immediately (delay=0)
  - t=2s: backend-api starts (2s delay from previous)
  - t=5s: frontend-server scheduled (3s delay from previous)
- `sync_delay` controls whether to wait for process startup:
  - `true`: Wait for process to be running before continuing
  - `false`: Schedule process and continue immediately

**Async mode behavior (`sync_delay: false`):**
- The tool returns immediately after processing initial no-delay processes
- Processes with delay=0 at the start are launched immediately and show status "running"
- The first process with delay>0 and all subsequent processes show status "pending"
- All pending processes are scheduled in a background goroutine with proper sequential delays

Example: If you have [delay=0, delay=0, delay=2000, delay=0, delay=1000], the response will show:
- Process 0: "running" (started immediately)
- Process 1: "running" (started immediately)
- Process 2: "pending" (will start at t=2s)
- Process 3: "pending" (will start at t=2s, after process 2)
- Process 4: "pending" (will start at t=3s, 1s after process 3)

#### `get_partial_process_output`
Get incremental output from a process since last read (tail -f functionality) with optional smart delay.

| Argument   | Type    | Required | Description                           |
|------------|---------|----------|---------------------------------------|
| process_id | string  |   ✅     | Process identifier                    |
| streams    | string  |   ❌     | "stdout", "stderr", or "both" (default) |
| max_lines  | number  |   ❌     | Maximum lines to return               |
| delay      | number  |   ❌     | Delay before returning output in milliseconds (max: 2 minutes, early termination if process completes) |

**Examples:**
```json
{
  "tool": "get_partial_process_output",
  "args": {
    "process_id": "abc123",
    "streams": "both",
    "max_lines": 50
  }
}
```

```json
{
  "tool": "get_partial_process_output",
  "args": {
    "process_id": "abc123",
    "delay": 5000
  }
}
```

#### `get_full_process_output`
Get the complete output from a process (all data currently in memory) with optional smart delay.

| Argument   | Type    | Required | Description                           |
|------------|---------|----------|---------------------------------------|
| process_id | string  |   ✅     | Process identifier                    |
| streams    | string  |   ❌     | "stdout", "stderr", or "both" (default) |
| max_lines  | number  |   ❌     | Maximum lines to return               |
| delay      | number  |   ❌     | Delay before returning output in milliseconds (max: 2 minutes, early termination if process completes) |

**Examples:**
```json
{
  "tool": "get_full_process_output",
  "args": {
    "process_id": "abc123",
    "streams": "stdout"
  }
}
```

```json
{
  "tool": "get_full_process_output",
  "args": {
    "process_id": "abc123",
    "delay": 3000
  }
}
```

#### `send_process_input`
Send input data to a running process's stdin.

| Argument   | Type   | Required | Description                  |
|------------|--------|----------|------------------------------|
| process_id | string |   ✅     | Process identifier           |
| input      | string |   ✅     | Input data to send to stdin  |

**Example:**
```json
{
  "tool": "send_process_input",
  "args": {
    "process_id": "abc123",
    "input": "yes\n"
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
- **Ring Buffer Output**: Configurable ring buffer (default 10MB) prevents unbounded memory growth for long-running processes
- **Incremental Output**: Cursor-based reading tracks exact position in stdout/stderr streams with proper handling of discarded data
- **Full Output Access**: Can retrieve complete process output currently in memory or just incremental changes
- **Stdin Support**: Send input to running processes via stdin pipe with proper error handling
- **Memory Management**: Ring buffers automatically discard old data when size limit is reached, with 1-hour cleanup timeout
- **Background Cleanup**: Goroutine runs every 15 minutes to remove stale processes and free resources
- **Color-free Output**: Spawned processes have `NO_COLOR=1` and `TERM=dumb` environment variables set
- **Process Status Tracking**: Real-time monitoring of running, completed, failed, and killed processes
- **UUID Process IDs**: Each spawned process gets a unique identifier for tracking
- **Process Names**: Optional non-unique human-readable names for easy reference
- **Smart Delay System**: Process spawning supports sync/async delays (max 5 minutes), output tools support delays with early termination (max 2 minutes)
- **Pending Status**: Async delayed processes show "pending" status until delay completes and execution begins
- **Graceful Shutdown**: On termination, sidekick sends SIGTERM to all child process groups, waits up to 5 seconds for graceful shutdown, then sends SIGKILL to any remaining process groups
- **Process Group Management**: Each spawned process runs in its own process group, ensuring all child processes and descendants are properly cleaned up on termination

## 📝 Notes
- macOS only (uses `afplay` and `say` for notifications)
- Requires MCP-compatible client (e.g. Claude Code)
- Process output stored in configurable ring buffers (default 10MB) with automatic cleanup (1-hour timeout)
- Color output is automatically disabled for clean parsing
- Ring buffers prevent memory issues with long-running processes by discarding old data when limits are reached
- Processes support stdin input for interactive command execution

## 🧑‍💻 Development

### File Structure
- `sidekick/main.go` - MCP server setup and tool registration
- `sidekick/notifications.go` - Audio notification functionality
- `sidekick/processes.go` - Process management and output tracking
- `install.sh` - Installation script that builds and installs the binary

### Building Manually
If you prefer to build manually instead of using the install script:

```bash
cd sidekick
go build -o sidekick main.go processes.go notifications.go
```

## 📚 References
- [MCP Protocol Spec](https://modelcontextprotocol.io/)
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- [Claude Code](https://claude.ai/)