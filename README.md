# Sidekick Daemon

A Golang daemon that provides an MCP (Model Context Protocol) server for audio notifications and text-to-speech functionality, designed for use with Claude Code and other LLMs supporting MCP.

## 🚀 Features

- MCP server using [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- Exposes a `notifications_speak` tool for audio notifications
- Plays system sound and speaks text (macOS only)
- Text validation (max 50 words)

## 🛠️ Usage

This daemon exposes an MCP tool called `notifications_speak`.

### 🧩 Claude Code Integration (Official CLI Method)

1. **Add the server to Claude Code using the CLI:**
   - From the project root directory, run:

     ```bash
     claude mcp add sidekick go run "$(pwd)/sidekick/main.go"
     ```
     - `$(pwd)` automatically uses your current directory path
     - `sidekick` is the name for this server in Claude Code

   - To add with environment variables:

     ```bash
     claude mcp add sidekick -e KEY=value go run "$(pwd)/sidekick/main.go"
     ```

3. **Verify the server is added:**
   - Run:
     ```bash
     claude mcp list
     ```
   - You should see `sidekick` in the list.

4. **Use the tool in Claude Code:**
   - Open Claude Code in your project.
   - The `notifications_speak` tool will be auto-discovered and available in the tool palette or via `/` commands.
   - You can now call the tool from chat, automations, or workflows.

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

### 🛠️ Tool: `notifications_speak`

| Argument | Type   | Required | Description                  |
|----------|--------|----------|------------------------------|
| text     | string |   ✅     | Text to speak (max 50 words) |

#### Example MCP Tool Call (pseudo-JSON):

```json
{
  "tool": "notifications_speak",
  "args": {
    "text": "Task completed successfully!"
  }
}
```

#### Error Handling
- If `text` is missing or empty, returns an error.
- If `text` exceeds 50 words, returns an error.

## ⚙️ Technical Details

- Uses [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) for MCP server implementation
- Plays `/System/Library/Sounds/Glass.aiff` using `afplay`
- Uses `say -v "Zoe (Premium)"` for text-to-speech
- Both audio commands run concurrently
- Request returns immediately (MCP tool result) without waiting for audio completion

## 📝 Notes
- macOS only (uses `afplay` and `say`)
- Requires MCP-compatible client (e.g. Claude Code)

## 🧑‍💻 Development

- MCP server entrypoint: `sidekick/main.go`
- Tool logic: `notifications_speak` in `main.go`

## 📚 References
- [MCP Protocol Spec](https://modelcontextprotocol.io/)
- [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go)
- [Claude Code](https://claude.ai/)