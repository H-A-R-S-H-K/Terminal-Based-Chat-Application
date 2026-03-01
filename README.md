# Echo — Terminal TCP Chat Application

A production-quality, multi-room terminal chat application in Go using **raw TCP sockets**. No WebSockets, no HTTP, no external dependencies — only the Go standard library.

## Features

- **Multi-room chat** — create, join, leave rooms
- **Concurrent clients** — goroutine-per-connection with buffered channels
- **Message history** — full replay when joining a room
- **Unique usernames** — with duplicate detection
- **Room listing** — see all rooms and occupancy
- **Graceful shutdown** — SIGINT/SIGTERM triggers clean client disconnect
- **ANSI-colored output** — broadcasts, history, errors, and system messages
- **Race-condition safe** — `sync.RWMutex` on all shared state; passes `go build -race`

## Architecture

```
Echo/
├── protocol/
│   └── protocol.go      # Shared wire protocol (commands, responses, encoding)
├── server/
│   ├── server.go         # Accept loop, command routing, shutdown
│   ├── client.go         # Per-client read/write goroutines
│   └── room.go           # Room state, broadcast, history
├── client/
│   └── client.go         # TCP client, ANSI display, input loop
└── cmd/
    ├── server/main.go    # Server entrypoint (flags, signals)
    └── client/main.go    # Client entrypoint (flags, banner)
```

### Concurrency Model

| Mechanism | Purpose |
|---|---|
| Goroutine per client (read) | Non-blocking reads from each TCP connection |
| Goroutine per client (write) | Drains buffered outgoing channel to conn |
| `sync.RWMutex` on server maps | Thread-safe room/client lookup and creation |
| `sync.RWMutex` on room maps | Thread-safe member add/remove and broadcast |
| Buffered `chan string` (256) | Decouples broadcast from slow writers |
| `context.Context` | Propagates shutdown signal to all goroutines |
| `sync.WaitGroup` | Waits for all goroutines before process exit |
| `sync.Once` | Ensures `Disconnect()` is idempotent |

### Message Protocol

All messages are newline-delimited UTF-8 text.

**Client → Server:**

```
NICK <username>
CREATE <room_name>
JOIN <room_name>
LEAVE
MSG <message text>
LIST
QUIT
```

**Server → Client:**

```
OK <detail>                           # Success response
ERR <detail>                          # Error response
BROADCAST [room][user]: message       # Chat message
HISTORY [room][user]: message         # Replayed history entry
SYS *** info message                  # System notification
```

## Quick Start

### Start the Server

```bash
go run ./cmd/server --port 9000
```

### Connect a Client

```bash
go run ./cmd/client --addr localhost:9000 --nick Alice
```

### Example Session

**Terminal 1 (Alice):**
```
╔══════════════════════════════════════╗
║           ◈  E C H O  ◈             ║
║      Terminal Chat Application       ║
╚══════════════════════════════════════╝

Connecting to localhost:9000...

*** Welcome to Echo! Your ID is user1. Use NICK <name> to set your username.
✓ Username set to 'Alice'
CREATE lobby
✓ Room 'lobby' created
JOIN lobby
*** Alice has joined the room
✓ Joined room 'lobby'
MSG Hello everyone!
[lobby][Alice]: Hello everyone!
```

**Terminal 2 (Bob):**
```bash
go run ./cmd/client --addr localhost:9000 --nick Bob
```

*** Welcome to Echo! Your ID is user2. Use NICK <name> to set your username.
✓ Username set to 'Bob'
JOIN lobby
[lobby][Alice]: Hello everyone!          ← history replay (gray)
*** Bob has joined the room
✓ Joined room 'lobby'
MSG Hey Alice!
[lobby][Bob]: Hey Alice!
LIST
Available rooms:
  • lobby (2/50 users)
LEAVE
✓ Left room 'lobby'
QUIT
Disconnected. Goodbye!
```

## Build

```bash
# Standard build
go build ./cmd/server
go build ./cmd/client

# With race detector
go build -race ./cmd/server
go build -race ./cmd/client
```

## Configuration

| Flag | Default | Description |
|---|---|---|
| `--port` (server) | `9000` | TCP port to listen on |
| `--addr` (client) | `localhost:9000` | Server address |
| `--nick` (client) | *(auto-assigned)* | Username on connect |

## Limits

| Parameter | Value |
|---|---|
| Max clients per room | 50 |
| Max message history per room | 1000 messages |
| Max message size | 64 KB |
| Outgoing buffer per client | 256 messages |
