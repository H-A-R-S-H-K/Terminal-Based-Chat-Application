// Package discovery implements LAN room discovery for Echo using UDP broadcast.
// A host broadcasts its room info every second, and listeners on the same
// network automatically discover available rooms.
package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	// BroadcastAddr is the UDP broadcast destination used for room announcements.
	BroadcastAddr = "255.255.255.255:9999"

	// BroadcastPrefix is the protocol prefix for room advertisements.
	BroadcastPrefix = "ECHO_ROOM"

	// BroadcastInterval is how often a host advertises its room.
	BroadcastInterval = 1 * time.Second
)

// RoomBroadcaster periodically sends UDP broadcast packets advertising a
// hosted room so that other clients on the same LAN can discover it.
type RoomBroadcaster struct {
	roomName string
	tcpPort  int
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewBroadcaster creates a broadcaster that advertises the given room name and
// TCP port. Call Start to begin broadcasting, and Stop to shut down.
func NewBroadcaster(roomName string, tcpPort int) *RoomBroadcaster {
	ctx, cancel := context.WithCancel(context.Background())
	return &RoomBroadcaster{
		roomName: roomName,
		tcpPort:  tcpPort,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins broadcasting room info in a background goroutine.
func (b *RoomBroadcaster) Start() error {
	// Use ListenPacket so we can set the broadcast flag on all platforms.
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return fmt.Errorf("broadcaster: listen: %w", err)
	}

	dst, err := net.ResolveUDPAddr("udp4", BroadcastAddr)
	if err != nil {
		conn.Close()
		return fmt.Errorf("broadcaster: resolve: %w", err)
	}

	msg := []byte(fmt.Sprintf("%s %s %d\n", BroadcastPrefix, b.roomName, b.tcpPort))

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer conn.Close()

		ticker := time.NewTicker(BroadcastInterval)
		defer ticker.Stop()

		// Send an initial broadcast immediately.
		if _, err := conn.WriteTo(msg, dst); err != nil {
			log.Printf("[broadcaster] initial send error: %v", err)
		}

		for {
			select {
			case <-b.ctx.Done():
				return
			case <-ticker.C:
				if _, err := conn.WriteTo(msg, dst); err != nil {
					log.Printf("[broadcaster] send error: %v", err)
				}
			}
		}
	}()

	log.Printf("[broadcaster] advertising room %q on port %d", b.roomName, b.tcpPort)
	return nil
}

// Stop cancels the broadcast loop and waits for the goroutine to finish.
func (b *RoomBroadcaster) Stop() {
	b.cancel()
	b.wg.Wait()
	log.Printf("[broadcaster] stopped advertising room %q", b.roomName)
}
