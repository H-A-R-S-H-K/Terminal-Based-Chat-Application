// Command server starts the Echo chat server.
//
// Usage:
//
//	go run ./cmd/server [--port PORT]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/harshkumar/echo/server"
)

func main() {
	port := flag.Int("port", 9000, "TCP port to listen on")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)

	srv := server.New()
	if err := srv.Start(addr); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}

	// Block until SIGINT or SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[server] received signal: %s", sig)

	srv.Shutdown()
}
