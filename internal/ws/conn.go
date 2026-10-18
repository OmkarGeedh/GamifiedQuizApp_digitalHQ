package ws

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	WriteWait      = 10 * time.Second
	PongWait       = 60 * time.Second
	PingInterval   = (PongWait * 9) / 10
	MaxMessageSize = 4096
	SendBufferSize = 256
)

// PlayerConn represents a connected player's WebSocket connection with a dedicated
// non-blocking write pump to decouple the game session event loop from network I/O.
type PlayerConn struct {
	Conn      *websocket.Conn
	ClientID  int
	send      chan interface{}
	closed    chan struct{}
	closeOnce sync.Once
}

// NewPlayerConn creates a wrapped thread-safe player connection and starts its dedicated write pump.
func NewPlayerConn(conn *websocket.Conn, clientID int) *PlayerConn {
	p := &PlayerConn{
		Conn:     conn,
		ClientID: clientID,
		send:     make(chan interface{}, SendBufferSize),
		closed:   make(chan struct{}),
	}
	go p.writePump()
	return p
}

// writePump drains outgoing messages and emits periodic pings in a single dedicated writer goroutine.
func (p *PlayerConn) writePump() {
	ticker := time.NewTicker(PingInterval)
	defer func() {
		ticker.Stop()
		_ = p.Conn.Close()
	}()

	for {
		select {
		case <-p.closed:
			return
		case msg, ok := <-p.send:
			if !ok {
				return
			}
			_ = p.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := p.Conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = p.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := p.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// WriteJSON safely enqueues a JSON message for transmission without blocking the caller.
func (p *PlayerConn) WriteJSON(v interface{}) error {
	select {
	case <-p.closed:
		return errors.New("connection closed")
	default:
	}

	select {
	case p.send <- v:
		return nil
	default:
		return errors.New("send buffer full")
	}
}

// Close terminates the write pump and closes the underlying socket.
func (p *PlayerConn) Close() {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.Conn.Close()
	})
}

// CloseGracefully sends an RFC 6455 Close frame and terminates the socket connection.
func (p *PlayerConn) CloseGracefully(reason string) {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.Conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason),
			time.Now().Add(WriteWait),
		)
		_ = p.Conn.Close()
	})
}
