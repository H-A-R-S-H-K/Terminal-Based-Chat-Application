package client

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/harshkumar/echo/protocol"
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
type Client struct {
	conn   net.Conn
	done   chan struct{}
}

// New dials the server at the given address and returns a connected Client.
func New(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return &Client{
		conn: conn,
		done: make(chan struct{}),
	}, nil
}

// Run starts the receive goroutine and enters the interactive input loop.
// It blocks until the user quits or the connection is closed.
func (c *Client) Run() {
	go c.receiveLoop()
	c.inputLoop()
}

// Close shuts down the client connection.
func (c *Client) Close() {
	select {
	case <-c.done:
		// Already closed.
	default:
		close(c.done)
	}
	c.conn.Close()
}

// SendLine writes a raw line to the server.
func (c *Client) SendLine(line string) error {
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

// inputLoop reads lines from stdin and sends them to the server.
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

		// Handle QUIT locally as well.
		verb, _ := protocol.ParseCommand(line)
		if verb == protocol.CmdQuit {
			c.SendLine(line)
			c.Close()
			return
		}

		if err := c.SendLine(line); err != nil {
			fmt.Fprintf(os.Stderr, "%s%ssend error: %v%s\n", colorBold, colorRed, err, colorReset)
			c.Close()
			return
		}
	}
}
