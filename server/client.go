package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/harshkumar/echo/protocol"
)

// Client represents a single connected user.
type Client struct {
	ID       string
	Username string
	Conn     net.Conn
	Room     *Room       // nil when not in any room
	outgoing chan string  // buffered channel drained by the write goroutine
	done     chan struct{}
	once     sync.Once
	server   *Server
}

const outgoingBufSize = 256

// NewClient creates a new Client bound to the given connection and server.
func NewClient(id string, conn net.Conn, srv *Server) *Client {
	return &Client{
		ID:       id,
		Username: id, // default username is the generated ID
		Conn:     conn,
		outgoing: make(chan string, outgoingBufSize),
		done:     make(chan struct{}),
		server:   srv,
	}
}

// Send enqueues a message for the client's write goroutine. If the buffer is
// full the message is dropped to avoid blocking the broadcaster.
func (c *Client) Send(msg string) {
	select {
	case c.outgoing <- msg:
	default:
		// Slow consumer — drop the message rather than blocking.
		log.Printf("[warn] dropping message for slow client %s", c.Username)
	}
}

// readLoop reads lines from the TCP connection and dispatches them to the server.
func (c *Client) readLoop() {
	defer c.Disconnect()

	scanner := bufio.NewScanner(c.Conn)
	// Allow messages up to 64 KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	for scanner.Scan() {
		select {
		case <-c.done:
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		verb, arg := protocol.ParseCommand(line)
		c.server.handleCommand(c, verb, arg)

		if verb == protocol.CmdQuit {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[info] client %s read error: %v", c.Username, err)
	}
}

// writeLoop drains the outgoing channel and writes to the TCP connection.
func (c *Client) writeLoop() {
	for {
		select {
		case msg, ok := <-c.outgoing:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(c.Conn, msg); err != nil {
				log.Printf("[info] client %s write error: %v", c.Username, err)
				return
			}
		case <-c.done:
			// Drain remaining messages before exiting.
			for {
				select {
				case msg, ok := <-c.outgoing:
					if !ok {
						return
					}
					fmt.Fprint(c.Conn, msg)
				default:
					return
				}
			}
		}
	}
}

// Disconnect performs a clean teardown of the client. Safe to call multiple times.
func (c *Client) Disconnect() {
	c.once.Do(func() {
		close(c.done)

		// Leave current room.
		if c.Room != nil {
			c.Room.RemoveClient(c)
			c.Room = nil
		}

		// Close the connection (unblocks scanner in readLoop).
		c.Conn.Close()

		// Close outgoing channel so writeLoop exits.
		close(c.outgoing)

		// Remove from server registry.
		c.server.removeClient(c)

		log.Printf("[info] client %s disconnected", c.Username)
	})
}
