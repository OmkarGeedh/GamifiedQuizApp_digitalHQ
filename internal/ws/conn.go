package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	WriteWait      = 10 * time.Second
	PongWait       = 60 * time.Second
	PingInterval   = (PongWait * 9) / 10
	MaxMessageSize = 4096
)

// PlayerConn represents a connected player's WebSocket connection with synchronized writes.
type PlayerConn struct {
	Conn     *websocket.Conn
	ClientID int
	mu       sync.Mutex
}

// NewPlayerConn creates a wrapped thread-safe player connection.
func NewPlayerConn(conn *websocket.Conn, clientID int) *PlayerConn {
	return &PlayerConn{
		Conn:     conn,
		ClientID: clientID,
	}
}

// WriteJSON safely writes a JSON message to the client, enforcing a write deadline.
func (p *PlayerConn) WriteJSON(v interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
	return p.Conn.WriteJSON(v)
}

// CloseGracefully sends an RFC 6455 Close frame and terminates the socket connection.
func (p *PlayerConn) CloseGracefully(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.Conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason),
		time.Now().Add(WriteWait),
	)
	_ = p.Conn.Close()
}
