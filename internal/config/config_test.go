package config

import (
	"testing"
)

func TestDatabaseConfig_ConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		cfg      DatabaseConfig
		expected string
	}{
		{
			name: "Explicit URL takes precedence",
			cfg: DatabaseConfig{
				URL:  "postgres://custom:pass@remote:5432/customdb?sslmode=require",
				Host: "localhost",
				Port: "5432",
			},
			expected: "postgres://custom:pass@remote:5432/customdb?sslmode=require",
		},
		{
			name: "Constructed from components when URL is empty",
			cfg: DatabaseConfig{
				User:     "user1",
				Password: "password1",
				Host:     "dbhost",
				Port:     "5433",
				Name:     "quizdb",
			},
			expected: "postgres://user1:password1@dbhost:5433/quizdb?sslmode=disable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.cfg.ConnectionString()
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestRedisConfig_Endpoint(t *testing.T) {
	tests := []struct {
		name     string
		cfg      RedisConfig
		expected string
	}{
		{
			name: "Explicit Addr takes precedence",
			cfg: RedisConfig{
				Addr: "redis-cluster:6380",
				Host: "localhost",
				Port: "6379",
			},
			expected: "redis-cluster:6380",
		},
		{
			name: "Constructed from host and port",
			cfg: RedisConfig{
				Host: "myredis",
				Port: "6381",
			},
			expected: "myredis:6381",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.cfg.Endpoint()
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
