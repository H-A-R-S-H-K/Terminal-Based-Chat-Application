// Command client connects to the Echo chat server or operates in standalone
// peer-discovery mode.
//
// Usage:
//
//	go run ./cmd/client [--addr HOST:PORT] [--nick USERNAME]
//
// If --addr is omitted the client starts in standalone mode where you can
// HOST rooms, DISCOVER peers on the LAN, and JOIN <ip> <port> directly.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/harshkumar/echo/client"
	"github.com/harshkumar/echo/discovery"
)

const banner = `
╔══════════════════════════════════════╗
║           ◈  E C H O  ◈             ║
║      Terminal Chat Application       ║
╚══════════════════════════════════════╝

Commands:
  NICK     <name>        Set your username
  CREATE   <room>        Create a new room
  JOIN     <room>        Join an existing room on current server
  LEAVE                  Leave your current room
  MSG      <text>        Send a message to your room
  LIST                   List rooms on current server
  HOST     <room> <port> Host a room (starts server + LAN broadcast)
  DISCOVER               Show rooms discovered on LAN
  JOIN     <ip> <port>   Connect to a peer-hosted room
  QUIT                   Disconnect and exit

`

func main() {
	addr := flag.String("addr", "", "Server address (host:port). Omit to start in standalone mode.")
	nick := flag.String("nick", "", "Set username on connect")
	flag.Parse()

	fmt.Print(banner)

	// Start the LAN discovery listener (always on).
	dl := discovery.NewListener()
	if err := dl.Start(); err != nil {
		log.Printf("[warn] LAN discovery unavailable: %v", err)
		dl = nil
	}

	var c *client.Client
	var err error

	if *addr != "" {
		// Connect to a centralized server.
		fmt.Printf("Connecting to %s...\n\n", *addr)
		c, err = client.New(*addr, dl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Standalone mode: can HOST, DISCOVER, and JOIN later.
		fmt.Println("Starting in standalone mode. Use HOST or DISCOVER to get started.")
		c = client.NewStandalone(dl)
	}

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		c.Close()
		if dl != nil {
			dl.Stop()
		}
		os.Exit(0)
	}()

	defer func() {
		c.Close()
		if dl != nil {
			dl.Stop()
		}
	}()

	// Send NICK command if provided via flag.
	if *nick != "" && c.IsConnected() {
		if err := c.SendLine(fmt.Sprintf("NICK %s", *nick)); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting nick: %v\n", err)
			os.Exit(1)
		}
	}

	c.Run()
	fmt.Println("\nDisconnected. Goodbye!")
}
