package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// ListenPort is the UDP port where room broadcasts are received.
	ListenPort = 9999

	// StaleTimeout is how long a room can go without a broadcast before it
	// is considered stale and removed from the discovered list.
	StaleTimeout = 5 * time.Second

	// cleanupInterval is how often the stale-room cleanup runs.
	cleanupInterval = 1 * time.Second
)

// RoomInfo holds the metadata for a single discovered room on the LAN.
type RoomInfo struct {
	Name     string
	Host     string // IP address of the host
	Port     int
	LastSeen time.Time
}

// RoomListener listens for UDP broadcast room advertisements on the LAN and
// maintains a thread-safe registry of discovered rooms.
type RoomListener struct {
	rooms  map[string]RoomInfo // keyed by "host:port"
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewListener creates a new RoomListener. Call Start to begin listening.
func NewListener() *RoomListener {
	ctx, cancel := context.WithCancel(context.Background())
	return &RoomListener{
		rooms:  make(map[string]RoomInfo),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start opens a UDP socket on ListenPort and begins receiving room broadcasts
// in the background. It also starts a cleanup goroutine to prune stale rooms.
func (l *RoomListener) Start() error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", ListenPort))
	if err != nil {
		return fmt.Errorf("listener: resolve: %w", err)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listener: listen: %w", err)
	}

	// Receive loop.
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer conn.Close()

		buf := make([]byte, 1024)
		for {
			select {
			case <-l.ctx.Done():
				return
			default:
			}

			// Set a read deadline so we periodically check for cancellation.
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				select {
				case <-l.ctx.Done():
					return
				default:
					log.Printf("[listener] read error: %v", err)
					continue
				}
			}

			l.handlePacket(buf[:n], src)
		}
	}()

	// Stale room cleanup loop.
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()

		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-l.ctx.Done():
				return
			case <-ticker.C:
				l.pruneStale()
			}
		}
	}()

	log.Printf("[listener] listening for room broadcasts on UDP port %d", ListenPort)
	return nil
}

// Stop cancels the listener and waits for all goroutines to finish.
func (l *RoomListener) Stop() {
	l.cancel()
	l.wg.Wait()
	log.Printf("[listener] stopped")
}

// Rooms returns a snapshot of all currently discovered rooms, sorted by name.
func (l *RoomListener) Rooms() []RoomInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]RoomInfo, 0, len(l.rooms))
	for _, info := range l.rooms {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// handlePacket parses a single UDP broadcast packet and upserts the room
// into the discovered registry.
func (l *RoomListener) handlePacket(data []byte, src *net.UDPAddr) {
	// Expected format: "ECHO_ROOM <name> <port>\n"
	line := strings.TrimSpace(string(data))
	parts := strings.Fields(line)
	if len(parts) != 3 || parts[0] != BroadcastPrefix {
		return
	}

	roomName := parts[1]
	port, err := strconv.Atoi(parts[2])
	if err != nil || port <= 0 || port > 65535 {
		return
	}

	hostIP := src.IP.String()
	key := fmt.Sprintf("%s:%d", hostIP, port)

	l.mu.Lock()
	l.rooms[key] = RoomInfo{
		Name:     roomName,
		Host:     hostIP,
		Port:     port,
		LastSeen: time.Now(),
	}
	l.mu.Unlock()
}

// pruneStale removes room entries that haven't been refreshed within StaleTimeout.
func (l *RoomListener) pruneStale() {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	for key, info := range l.rooms {
		if now.Sub(info.LastSeen) > StaleTimeout {
			delete(l.rooms, key)
		}
	}
}
