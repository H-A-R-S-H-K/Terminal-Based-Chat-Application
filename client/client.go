package client

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/harshkumar/echo/discovery"
	"github.com/harshkumar/echo/protocol"
	"github.com/harshkumar/echo/server"
)

// ── ANSI color codes ────────────────────────────────────────────────────────

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// Client manages the TCP connection to the Echo server and the terminal UI.
// It also supports peer-hosted rooms and LAN discovery.
type Client struct {
	conn     net.Conn
	done     chan struct{}
	listener *discovery.RoomListener

	// Hosting state — protected by hostMu.
	hostMu      sync.Mutex
	hostedSrv   *server.Server
	broadcaster *discovery.RoomBroadcaster
}

// New dials the server at the given address and returns a connected Client.
func New(addr string, listener *discovery.RoomListener) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &Client{
		conn:     conn,
		done:     make(chan struct{}),
		listener: listener,
	}, nil
}

// NewStandalone creates a Client that is not connected to any server.
// It can discover and host rooms, or connect later via JOIN <ip> <port>.
func NewStandalone(listener *discovery.RoomListener) *Client {
	return &Client{
		done:     make(chan struct{}),
		listener: listener,
	}
}

// Run starts the receive goroutine (if connected) and enters the interactive
// input loop. It blocks until the user quits or the connection is closed.
func (c *Client) Run() {
	if c.conn != nil {
		go c.receiveLoop()
	}
	c.inputLoop()
}

// Close shuts down the client connection and any hosted resources.
func (c *Client) Close() {
	select {
	case <-c.done:
		// Already closed.
	default:
		close(c.done)
	}
	if c.conn != nil {
		c.conn.Close()
	}
	c.stopHosting()
}

// SendLine writes a raw line to the server.
func (c *Client) SendLine(line string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected to any server")
	}
	_, err := fmt.Fprintf(c.conn, "%s\n", line)
	return err
}

// receiveLoop reads lines from the server and prints them to stdout with
// color formatting based on the response prefix.
func (c *Client) receiveLoop() {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	for scanner.Scan() {
		select {
		case <-c.done:
			return
		default:
		}

		line := scanner.Text()
		c.display(line)
	}

	// Connection closed by server or error.
	select {
	case <-c.done:
	default:
		fmt.Printf("\n%s%s*** Connection closed by server%s\n", colorBold, colorRed, colorReset)
		close(c.done)
	}
}

// display parses a server response line and prints it with ANSI colors.
func (c *Client) display(line string) {
	prefix, payload := protocol.ParseCommand(line)

	switch prefix {
	case protocol.RespBroadcast:
		fmt.Printf("%s%s%s\n", colorCyan, payload, colorReset)
	case protocol.RespHistory:
		fmt.Printf("%s%s%s\n", colorGray, payload, colorReset)
	case protocol.RespOK:
		fmt.Printf("%s%s✓ %s%s\n", colorBold, colorGreen, payload, colorReset)
	case protocol.RespErr:
		fmt.Printf("%s%s✗ %s%s\n", colorBold, colorRed, payload, colorReset)
	case protocol.RespSys:
		fmt.Printf("%s%s%s\n", colorYellow, payload, colorReset)
	default:
		fmt.Println(line)
	}
}

// inputLoop reads lines from stdin and routes them to the server or handles
// them locally (HOST, DISCOVER, JOIN <ip> <port>).
func (c *Client) inputLoop() {
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-c.done:
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// User pressed Ctrl+D.
				c.SendLine(protocol.CmdQuit)
				c.Close()
				return
			}
			fmt.Fprintf(os.Stderr, "input error: %v\n", err)
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		verb, arg := protocol.ParseCommand(line)

		// ── Local-only commands ─────────────────────────────────────
		switch verb {
		case protocol.CmdHost:
			c.handleHost(arg)
			continue
		case protocol.CmdDiscover:
			c.handleDiscover()
			continue
		case protocol.CmdQuit:
			c.SendLine(line)
			c.Close()
			return
		}

		// Check for JOIN <ip> <port> (two-arg peer join).
		if verb == protocol.CmdJoin {
			parts := strings.Fields(arg)
			if len(parts) == 2 {
				if _, err := strconv.Atoi(parts[1]); err == nil {
					c.handlePeerJoin(parts[0], parts[1])
					continue
				}
			}
		}

		// ── Forward everything else to the connected server ─────────
		if c.conn == nil {
			fmt.Printf("%s%s✗ Not connected. Use HOST, DISCOVER, or JOIN <ip> <port>.%s\n",
				colorBold, colorRed, colorReset)
			continue
		}

		if err := c.SendLine(line); err != nil {
			fmt.Fprintf(os.Stderr, "%s%ssend error: %v%s\n", colorBold, colorRed, err, colorReset)
			c.Close()
			return
		}
	}
}

// ── Local command handlers ──────────────────────────────────────────────────

// handleHost starts a local TCP server and UDP broadcaster for a peer-hosted room.
// Usage: HOST <room_name> <port>
func (c *Client) handleHost(arg string) {
	parts := strings.Fields(arg)
	if len(parts) != 2 {
		fmt.Printf("%s%s✗ Usage: HOST <room_name> <port>%s\n", colorBold, colorRed, colorReset)
		return
	}

	roomName := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil || port <= 0 || port > 65535 {
		fmt.Printf("%s%s✗ Invalid port: %s%s\n", colorBold, colorRed, parts[1], colorReset)
		return
	}

	c.hostMu.Lock()
	if c.hostedSrv != nil {
		c.hostMu.Unlock()
		fmt.Printf("%s%s✗ Already hosting a room. Stop the current host first.%s\n",
			colorBold, colorRed, colorReset)
		return
	}
	c.hostMu.Unlock()

	// 1. Start the TCP server.
	srv := server.New()
	addr := fmt.Sprintf(":%d", port)
	if err := srv.Start(addr); err != nil {
		fmt.Printf("%s%s✗ Failed to start server: %v%s\n", colorBold, colorRed, err, colorReset)
		return
	}

	// 2. Start the UDP broadcaster.
	bc := discovery.NewBroadcaster(roomName, port)
	if err := bc.Start(); err != nil {
		srv.Shutdown()
		fmt.Printf("%s%s✗ Failed to start broadcaster: %v%s\n", colorBold, colorRed, err, colorReset)
		return
	}

	c.hostMu.Lock()
	c.hostedSrv = srv
	c.broadcaster = bc
	c.hostMu.Unlock()

	fmt.Printf("%s%s✓ Hosting room '%s' on port %d%s\n",
		colorBold, colorGreen, roomName, port, colorReset)

	// 3. Connect to our own server.
	if c.conn != nil {
		c.conn.Close()
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		fmt.Printf("%s%s✗ Failed to connect to own server: %v%s\n",
			colorBold, colorRed, err, colorReset)
		return
	}
	c.conn = conn

	// Start the receive loop for the new connection.
	go c.receiveLoop()

	fmt.Printf("%s%s✓ Connected to your hosted room. Use CREATE %s then JOIN %s.%s\n",
		colorBold, colorGreen, roomName, roomName, colorReset)
}

// handleDiscover prints all currently discovered rooms from the LAN listener.
func (c *Client) handleDiscover() {
	if c.listener == nil {
		fmt.Printf("%s%s✗ Discovery not available%s\n", colorBold, colorRed, colorReset)
		return
	}

	rooms := c.listener.Rooms()
	if len(rooms) == 0 {
		fmt.Printf("%sNo rooms discovered on the network.%s\n", colorYellow, colorReset)
		return
	}

	fmt.Printf("%s%sAvailable rooms:%s\n", colorBold, colorYellow, colorReset)
	for _, r := range rooms {
		fmt.Printf("  %s•%s %s%s%s (%s:%d)\n",
			colorGreen, colorReset,
			colorCyan, r.Name, colorReset,
			r.Host, r.Port)
	}
}

// handlePeerJoin connects directly to a peer host.
// Usage: JOIN <ip> <port>
func (c *Client) handlePeerJoin(ip, portStr string) {
	addr := fmt.Sprintf("%s:%s", ip, portStr)
	fmt.Printf("Connecting to %s...\n", addr)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("%s%s✗ Failed to connect: %v%s\n", colorBold, colorRed, err, colorReset)
		return
	}

	// Close old connection if any.
	if c.conn != nil {
		c.conn.Close()
	}

	c.conn = conn
	go c.receiveLoop()

	fmt.Printf("%s%s✓ Connected to %s%s\n", colorBold, colorGreen, addr, colorReset)
}

// stopHosting tears down a hosted server and its broadcaster if active.
func (c *Client) stopHosting() {
	c.hostMu.Lock()
	defer c.hostMu.Unlock()

	if c.broadcaster != nil {
		c.broadcaster.Stop()
		c.broadcaster = nil
	}
	if c.hostedSrv != nil {
		c.hostedSrv.Shutdown()
		c.hostedSrv = nil
	}
}

// IsConnected reports whether the client has an active TCP connection.
func (c *Client) IsConnected() bool {
	return c.conn != nil
}

// StopHosting is an exported wrapper for cleaning up hosted resources.
func (c *Client) StopHosting() {
	c.stopHosting()
}

// SetListener sets the discovery listener for this client.
func (c *Client) SetListener(l *discovery.RoomListener) {
	c.listener = l
}

// Logger is used to suppress server logs when hosting from the client.
func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
}
