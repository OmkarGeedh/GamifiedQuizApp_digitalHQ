package ws

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Platform represents the detected cloud hosting platform.
type Platform string

const (
	PlatformZoho    Platform = "zoho"
	PlatformAWS     Platform = "aws"
	PlatformGCP     Platform = "gcp"
	PlatformGeneric Platform = "generic"
)

const (
	WriteWait      = 10 * time.Second
	MaxMessageSize = 4096
	SendBufferSize = 256
)

var (
	// CurrentPlatform is the active cloud platform detected at runtime.
	CurrentPlatform = PlatformGeneric

	// PongWait is the maximum time allowed to read the next pong message from the peer.
	PongWait = 60 * time.Second

	// PingInterval is the frequency of outbound ping frames sent to keep the connection alive.
	PingInterval = 54 * time.Second

	// TCPKeepAlive is the duration for TCP-level keepalive probes on underlying connections.
	TCPKeepAlive = 30 * time.Second
)

func init() {
	InitPlatformTiming()
}

// DetectPlatform determines the deployment environment from explicit variables or runtime signatures.
func DetectPlatform() Platform {
	// 1. Explicit override via CLOUD_PROVIDER or PLATFORM
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("CLOUD_PROVIDER")))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM")))
	}

	switch provider {
	case "zoho", "zoho_catalyst", "catalyst":
		return PlatformZoho
	case "aws", "amazon":
		return PlatformAWS
	case "gcp", "google":
		return PlatformGCP
	case "generic", "standard", "docker", "local":
		return PlatformGeneric
	}

	// 2. Auto-detection via platform-injected environment variables
	if os.Getenv("X_ZOHO_CATALYST_LISTEN_PORT") != "" || os.Getenv("CATALYST_ENV") != "" {
		return PlatformZoho
	}
	if os.Getenv("AWS_REGION") != "" || os.Getenv("AWS_EXECUTION_ENV") != "" || os.Getenv("ECS_CONTAINER_METADATA_URI") != "" {
		return PlatformAWS
	}
	if os.Getenv("K_SERVICE") != "" || os.Getenv("GOOGLE_CLOUD_PROJECT") != "" {
		return PlatformGCP
	}

	return PlatformGeneric
}

// InitPlatformTiming configures WebSocket timing parameters based on the detected platform and environment overrides.
func InitPlatformTiming() {
	CurrentPlatform = DetectPlatform()

	switch CurrentPlatform {
	case PlatformZoho:
		// Zoho Catalyst AppSail enforces a strict 30s request/idle timeout on edge proxies.
		// Sending heartbeats every 15s keeps the persistent TCP tunnel alive.
		PongWait = 30 * time.Second
		PingInterval = 15 * time.Second
		TCPKeepAlive = 15 * time.Second

	case PlatformAWS:
		// AWS Application Load Balancers (ALB) and API Gateway support standard/longer idle timeouts.
		// Standard 54s ping / 60s pong wait optimizes network overhead.
		PongWait = 60 * time.Second
		PingInterval = 54 * time.Second
		TCPKeepAlive = 30 * time.Second

	case PlatformGCP:
		// GCP Cloud Run / Load Balancing default timeouts (60s).
		PongWait = 60 * time.Second
		PingInterval = 54 * time.Second
		TCPKeepAlive = 30 * time.Second

	default:
		// Generic / Local / Docker Compose default
		PongWait = 60 * time.Second
		PingInterval = 54 * time.Second
		TCPKeepAlive = 30 * time.Second
	}

	// Allow manual granular overrides via environment variables
	if v := os.Getenv("WS_PONG_WAIT_SECONDS"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			PongWait = time.Duration(sec) * time.Second
		}
	}
	if v := os.Getenv("WS_PING_INTERVAL_SECONDS"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			PingInterval = time.Duration(sec) * time.Second
		}
	}
	if v := os.Getenv("WS_TCP_KEEPALIVE_SECONDS"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			TCPKeepAlive = time.Duration(sec) * time.Second
		}
	}
}

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
