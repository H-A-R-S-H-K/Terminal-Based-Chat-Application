package server

import (
	"fmt"
	"sync"

	"github.com/harshkumar/echo/protocol"
)

const (
	// MaxClientsPerRoom is the maximum number of clients allowed in a single room.
	MaxClientsPerRoom = 50

	// maxHistory caps the number of stored messages per room.
	maxHistory = 1000
)

// Room represents a chat room that holds connected clients and message history.
type Room struct {
	Name    string
	clients map[string]*Client // keyed by client ID
	history []string           // formatted broadcast strings
	mu      sync.RWMutex
}

// NewRoom creates a new Room with the given name.
func NewRoom(name string) *Room {
	return &Room{
		Name:    name,
		clients: make(map[string]*Client),
		history: make([]string, 0, 256),
	}
}

// AddClient adds a client to the room after checking capacity. It replays the
// full message history to the joining client and notifies existing members.
func (r *Room) AddClient(c *Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.clients) >= MaxClientsPerRoom {
		return fmt.Errorf("room '%s' is full (%d/%d)", r.Name, len(r.clients), MaxClientsPerRoom)
	}

	// Replay history to the newly joined client.
	for _, msg := range r.history {
		c.Send(protocol.Encode(protocol.RespHistory, msg))
	}

	r.clients[c.ID] = c

	// Notify everyone in the room.
	sysMsg := protocol.FormatSystem(fmt.Sprintf("%s has joined the room", c.Username))
	r.broadcast(protocol.Encode(protocol.RespSys, sysMsg))

	return nil
}

// RemoveClient removes a client from the room and notifies remaining members.
func (r *Room) RemoveClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.clients[c.ID]; !ok {
		return
	}
	delete(r.clients, c.ID)

	sysMsg := protocol.FormatSystem(fmt.Sprintf("%s has left the room", c.Username))
	r.broadcast(protocol.Encode(protocol.RespSys, sysMsg))
}

// Broadcast sends a message from a client to every member of the room and
// appends it to the message history.
func (r *Room) Broadcast(c *Client, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	formatted := protocol.FormatBroadcast(r.Name, c.Username, message)

	// Store in history (with cap).
	if len(r.history) < maxHistory {
		r.history = append(r.history, formatted)
	}

	r.broadcast(protocol.Encode(protocol.RespBroadcast, formatted))
}

// ClientCount returns the number of clients in the room (thread-safe).
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// broadcast sends a raw line to every client in the room.
// Caller MUST hold r.mu.
func (r *Room) broadcast(line string) {
	for _, c := range r.clients {
		c.Send(line)
	}
}
