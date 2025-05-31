# Sidekick Daemon

A Golang daemon that provides HTTP endpoints for audio notifications and text-to-speech functionality, designed for use with Claude Code.

## Features

- HTTP server listening on port 12345
- POST endpoint `/notifications/speak` for audio notifications
- Plays system sound and speaks text simultaneously
- Text validation (max 50 words)

## Installation

### Build the binary

```bash
cd sidekick
go build -o bin/sidekick main.go
```

### Install as a system service (recommended)

To automatically start the daemon on boot:

```bash
cd sidekick
./scripts/install-service.sh
```

To uninstall the service:

```bash
cd sidekick
./scripts/uninstall-service.sh
```

### Manual startup

```bash
cd sidekick
./bin/sidekick
```

Or run from source:

```bash
cd sidekick
go run main.go
```

## Usage

### API Endpoints

#### POST /notifications/speak

Plays a system sound and speaks the provided text using text-to-speech.

**Request:**
```bash
curl -X POST http://localhost:12345/notifications/speak \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from Claude Code"}'
```

**Response:**
- `200 OK` - Success (no response body)
- `400 Bad Request` - Invalid JSON, missing text, or text exceeds 50 words

**Examples:**

Simple notification:
```bash
curl -X POST http://localhost:12345/notifications/speak \
  -H "Content-Type: application/json" \
  -d '{"text": "Task completed successfully"}'
```

Multiple word notification:
```bash
curl -X POST http://localhost:12345/notifications/speak \
  -H "Content-Type: application/json" \
  -d '{"text": "Build finished with no errors. All tests passed."}'
```

Error example (too many words):
```bash
curl -X POST http://localhost:12345/notifications/speak \
  -H "Content-Type: application/json" \
  -d '{"text": "This is a very long message that contains way more than fifty words and should trigger an error response from the server because it exceeds the maximum allowed word count limit that has been set to prevent overly long speech synthesis requests from being processed by the daemon"}'
```

## Technical Details

- Uses Echo web framework
- Plays `/System/Library/Sounds/Glass.aiff` using `afplay`
- Uses `say -v "Samantha (Enhanced)"` for text-to-speech
- Both audio commands run concurrently
- Request returns immediately (200 status) without waiting for audio completion