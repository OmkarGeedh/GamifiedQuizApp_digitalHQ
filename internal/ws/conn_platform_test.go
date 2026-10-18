package ws_test

import (
	"os"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/ws"
)

func TestDetectPlatform_And_Timing(t *testing.T) {
	// Preserve existing environment to restore after tests
	origProvider := os.Getenv("CLOUD_PROVIDER")
	origPlatform := os.Getenv("PLATFORM")
	origZohoPort := os.Getenv("X_ZOHO_CATALYST_LISTEN_PORT")
	origAwsRegion := os.Getenv("AWS_REGION")
	origGcpService := os.Getenv("K_SERVICE")
	origPing := os.Getenv("WS_PING_INTERVAL_SECONDS")
	origPong := os.Getenv("WS_PONG_WAIT_SECONDS")
	origKeepAlive := os.Getenv("WS_TCP_KEEPALIVE_SECONDS")

	defer func() {
		os.Setenv("CLOUD_PROVIDER", origProvider)
		os.Setenv("PLATFORM", origPlatform)
		os.Setenv("X_ZOHO_CATALYST_LISTEN_PORT", origZohoPort)
		os.Setenv("AWS_REGION", origAwsRegion)
		os.Setenv("K_SERVICE", origGcpService)
		os.Setenv("WS_PING_INTERVAL_SECONDS", origPing)
		os.Setenv("WS_PONG_WAIT_SECONDS", origPong)
		os.Setenv("WS_TCP_KEEPALIVE_SECONDS", origKeepAlive)
		ws.InitPlatformTiming()
	}()

	clearEnv := func() {
		os.Unsetenv("CLOUD_PROVIDER")
		os.Unsetenv("PLATFORM")
		os.Unsetenv("X_ZOHO_CATALYST_LISTEN_PORT")
		os.Unsetenv("CATALYST_ENV")
		os.Unsetenv("AWS_REGION")
		os.Unsetenv("AWS_EXECUTION_ENV")
		os.Unsetenv("ECS_CONTAINER_METADATA_URI")
		os.Unsetenv("K_SERVICE")
		os.Unsetenv("GOOGLE_CLOUD_PROJECT")
		os.Unsetenv("WS_PING_INTERVAL_SECONDS")
		os.Unsetenv("WS_PONG_WAIT_SECONDS")
		os.Unsetenv("WS_TCP_KEEPALIVE_SECONDS")
	}

	t.Run("Zoho Catalyst via CLOUD_PROVIDER", func(t *testing.T) {
		clearEnv()
		os.Setenv("CLOUD_PROVIDER", "zoho")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformZoho {
			t.Fatalf("expected PlatformZoho, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 15*time.Second {
			t.Fatalf("expected PingInterval 15s, got %v", ws.PingInterval)
		}
		if ws.PongWait != 30*time.Second {
			t.Fatalf("expected PongWait 30s, got %v", ws.PongWait)
		}
		if ws.TCPKeepAlive != 15*time.Second {
			t.Fatalf("expected TCPKeepAlive 15s, got %v", ws.TCPKeepAlive)
		}
	})

	t.Run("Zoho Catalyst via Auto-detected Port", func(t *testing.T) {
		clearEnv()
		os.Setenv("X_ZOHO_CATALYST_LISTEN_PORT", "8080")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformZoho {
			t.Fatalf("expected PlatformZoho, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 15*time.Second {
			t.Fatalf("expected PingInterval 15s, got %v", ws.PingInterval)
		}
	})

	t.Run("AWS via CLOUD_PROVIDER", func(t *testing.T) {
		clearEnv()
		os.Setenv("CLOUD_PROVIDER", "aws")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformAWS {
			t.Fatalf("expected PlatformAWS, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 54*time.Second {
			t.Fatalf("expected PingInterval 54s, got %v", ws.PingInterval)
		}
		if ws.PongWait != 60*time.Second {
			t.Fatalf("expected PongWait 60s, got %v", ws.PongWait)
		}
		if ws.TCPKeepAlive != 30*time.Second {
			t.Fatalf("expected TCPKeepAlive 30s, got %v", ws.TCPKeepAlive)
		}
	})

	t.Run("AWS via Auto-detected AWS_REGION", func(t *testing.T) {
		clearEnv()
		os.Setenv("AWS_REGION", "us-east-1")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformAWS {
			t.Fatalf("expected PlatformAWS, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 54*time.Second {
			t.Fatalf("expected PingInterval 54s, got %v", ws.PingInterval)
		}
	})

	t.Run("GCP via CLOUD_PROVIDER", func(t *testing.T) {
		clearEnv()
		os.Setenv("CLOUD_PROVIDER", "gcp")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformGCP {
			t.Fatalf("expected PlatformGCP, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 54*time.Second {
			t.Fatalf("expected PingInterval 54s, got %v", ws.PingInterval)
		}
	})

	t.Run("Generic fallback without cloud env", func(t *testing.T) {
		clearEnv()
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformGeneric {
			t.Fatalf("expected PlatformGeneric, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 54*time.Second {
			t.Fatalf("expected PingInterval 54s, got %v", ws.PingInterval)
		}
		if ws.PongWait != 60*time.Second {
			t.Fatalf("expected PongWait 60s, got %v", ws.PongWait)
		}
	})

	t.Run("Manual overrides take precedence", func(t *testing.T) {
		clearEnv()
		os.Setenv("CLOUD_PROVIDER", "zoho")
		os.Setenv("WS_PING_INTERVAL_SECONDS", "20")
		os.Setenv("WS_PONG_WAIT_SECONDS", "45")
		os.Setenv("WS_TCP_KEEPALIVE_SECONDS", "18")
		ws.InitPlatformTiming()

		if ws.CurrentPlatform != ws.PlatformZoho {
			t.Fatalf("expected PlatformZoho, got %s", ws.CurrentPlatform)
		}
		if ws.PingInterval != 20*time.Second {
			t.Fatalf("expected PingInterval 20s, got %v", ws.PingInterval)
		}
		if ws.PongWait != 45*time.Second {
			t.Fatalf("expected PongWait 45s, got %v", ws.PongWait)
		}
		if ws.TCPKeepAlive != 18*time.Second {
			t.Fatalf("expected TCPKeepAlive 18s, got %v", ws.TCPKeepAlive)
		}
	})
}
