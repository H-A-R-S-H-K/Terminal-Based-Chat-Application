package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/harshkumar/echo/protocol"
)

// Server is the central coordinator that manages rooms and client connections.
type Server struct {
	listener net.Listener
	rooms    map[string]*Room
	roomsMu  sync.RWMutex
	clients  map[string]*Client
	clientMu sync.RWMutex
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	nextID   atomic.Uint64
}

// New creates a new Server instance.
func New() *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		rooms:   make(map[string]*Room),
		clients: make(map[string]*Client),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins listening for TCP connections on the given address.
func (s *Server) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = ln
	log.Printf("[server] listening on %s", addr)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Shutdown performs a graceful shutdown: stops accepting new connections,
// disconnects every client, and waits for all goroutines to finish.
func (s *Server) Shutdown() {
	log.Println("[server] shutting down...")
	s.cancel()
	s.listener.Close()

	// Disconnect all clients.
	s.clientMu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.clientMu.RUnlock()

	for _, c := range clients {
		c.Send(protocol.Encode(protocol.RespSys, protocol.FormatSystem("Server is shutting down")))
		c.Disconnect()
	}

	s.wg.Wait()
	log.Println("[server] shutdown complete")
}

// acceptLoop runs in its own goroutine and accepts incoming TCP connections.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("[server] accept error: %v", err)
				continue
			}
		}

		id := fmt.Sprintf("user%d", s.nextID.Add(1))
		client := NewClient(id, conn, s)

		s.clientMu.Lock()
		s.clients[id] = client
		s.clientMu.Unlock()

		log.Printf("[server] client %s connected from %s", id, conn.RemoteAddr())

		// Send welcome message.
		client.Send(protocol.Encode(protocol.RespSys,
			protocol.FormatSystem(fmt.Sprintf("Welcome to Echo! Your ID is %s. Use NICK <name> to set your username.", id))))

		s.wg.Add(2)
		go func() {
			defer s.wg.Done()
			client.readLoop()
		}()
		go func() {
			defer s.wg.Done()
			client.writeLoop()
		}()
	}
}

// removeClient removes a client from the server's registry.
func (s *Server) removeClient(c *Client) {
	s.clientMu.Lock()
	delete(s.clients, c.ID)
	s.clientMu.Unlock()
}

// ── Command routing ─────────────────────────────────────────────────────────

func (s *Server) handleCommand(c *Client, verb, arg string) {
	switch verb {
	case protocol.CmdNick:
		s.handleNick(c, arg)
	case protocol.CmdCreate:
		s.handleCreate(c, arg)
	case protocol.CmdJoin:
		s.handleJoin(c, arg)
	case protocol.CmdLeave:
		s.handleLeave(c)
	case protocol.CmdMsg:
		s.handleMsg(c, arg)
	case protocol.CmdList:
		s.handleList(c)
	case protocol.CmdQuit:
		c.Send(protocol.Encode(protocol.RespOK, "Goodbye!"))
	default:
		c.Send(protocol.Encode(protocol.RespErr, fmt.Sprintf("Unknown command: %s", verb)))
	}
}

func (s *Server) handleNick(c *Client, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		c.Send(protocol.Encode(protocol.RespErr, "Usage: NICK <username>"))
		return
	}
	if len(name) > 32 {
		c.Send(protocol.Encode(protocol.RespErr, "Username must be 32 characters or less"))
		return
	}

	// Check for duplicate usernames.
	s.clientMu.RLock()
	for _, other := range s.clients {
		if other.ID != c.ID && strings.EqualFold(other.Username, name) {
			s.clientMu.RUnlock()
			c.Send(protocol.Encode(protocol.RespErr, fmt.Sprintf("Username '%s' is already taken", name)))
			return
		}
	}
	s.clientMu.RUnlock()

	old := c.Username
	c.Username = name
	log.Printf("[server] client %s changed nick: %s → %s", c.ID, old, name)
	c.Send(protocol.Encode(protocol.RespOK, fmt.Sprintf("Username set to '%s'", name)))
}

func (s *Server) handleCreate(c *Client, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		c.Send(protocol.Encode(protocol.RespErr, "Usage: CREATE <room_name>"))
		return
	}

	s.roomsMu.Lock()
	if _, exists := s.rooms[name]; exists {
		s.roomsMu.Unlock()
		c.Send(protocol.Encode(protocol.RespErr, fmt.Sprintf("Room '%s' already exists", name)))
		return
	}
	room := NewRoom(name)
	s.rooms[name] = room
	s.roomsMu.Unlock()

	log.Printf("[server] room '%s' created by %s", name, c.Username)
	c.Send(protocol.Encode(protocol.RespOK, fmt.Sprintf("Room '%s' created", name)))
}

func (s *Server) handleJoin(c *Client, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		c.Send(protocol.Encode(protocol.RespErr, "Usage: JOIN <room_name>"))
		return
	}

	// Leave current room first.
	if c.Room != nil {
		c.Room.RemoveClient(c)
		c.Room = nil
	}

	s.roomsMu.RLock()
	room, ok := s.rooms[name]
	s.roomsMu.RUnlock()

	if !ok {
		c.Send(protocol.Encode(protocol.RespErr, fmt.Sprintf("Room '%s' does not exist. Use CREATE first.", name)))
		return
	}

	if err := room.AddClient(c); err != nil {
		c.Send(protocol.Encode(protocol.RespErr, err.Error()))
		return
	}

	c.Room = room
	c.Send(protocol.Encode(protocol.RespOK, fmt.Sprintf("Joined room '%s'", name)))
}

func (s *Server) handleLeave(c *Client) {
	if c.Room == nil {
		c.Send(protocol.Encode(protocol.RespErr, "You are not in any room"))
		return
	}
	roomName := c.Room.Name
	c.Room.RemoveClient(c)
	c.Room = nil
	c.Send(protocol.Encode(protocol.RespOK, fmt.Sprintf("Left room '%s'", roomName)))
}

func (s *Server) handleMsg(c *Client, text string) {
	if c.Room == nil {
		c.Send(protocol.Encode(protocol.RespErr, "You must join a room first. Use JOIN <room_name>"))
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		c.Send(protocol.Encode(protocol.RespErr, "Usage: MSG <message>"))
		return
	}
	c.Room.Broadcast(c, text)
}

func (s *Server) handleList(c *Client) {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()

	if len(s.rooms) == 0 {
		c.Send(protocol.Encode(protocol.RespSys, "No rooms available. Use CREATE <room_name> to create one."))
		return
	}

	names := make([]string, 0, len(s.rooms))
	for name := range s.rooms {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Available rooms:\n")
	for _, name := range names {
		room := s.rooms[name]
		b.WriteString(fmt.Sprintf("  • %s (%d/%d users)\n", name, room.ClientCount(), MaxClientsPerRoom))
	}
	c.Send(protocol.Encode(protocol.RespSys, b.String()))
}
