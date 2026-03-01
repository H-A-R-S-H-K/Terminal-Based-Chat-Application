// Command client connects to the Echo chat server.
//
// Usage:
//
//	go run ./cmd/client [--addr HOST:PORT] [--nick USERNAME]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/harshkumar/echo/client"
)

const banner = `
╔══════════════════════════════════════╗
║           ◈  E C H O  ◈             ║
║      Terminal Chat Application       ║
╚══════════════════════════════════════╝

Commands:
  NICK   <name>     Set your username
  CREATE <room>     Create a new room
  JOIN   <room>     Join an existing room
  LEAVE             Leave your current room
  MSG    <text>     Send a message to your room
  LIST              List available rooms
  QUIT              Disconnect and exit

`

func main() {
	addr := flag.String("addr", "localhost:9000", "Server address (host:port)")
	nick := flag.String("nick", "", "Set username on connect")
	flag.Parse()

	fmt.Print(banner)
	fmt.Printf("Connecting to %s...\n\n", *addr)

	c, err := client.New(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	// Send NICK command if provided via flag.
	if *nick != "" {
		if err := c.SendLine(fmt.Sprintf("NICK %s", *nick)); err != nil {
			fmt.Fprintf(os.Stderr, "Error setting nick: %v\n", err)
			os.Exit(1)
		}
	}

	c.Run()
	fmt.Println("\nDisconnected. Goodbye!")
}
