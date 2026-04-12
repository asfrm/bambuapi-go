// Package sdk defines interfaces and types for the Bambu Lab printer SDK.
package sdk

import (
	"context"
	"time"
)

// Printer defines the interface for interacting with a Bambu Lab 3D printer.
type Printer interface {
	Connect() error
	Disconnect()
	GetConnectionState() ConnectionState
	GetHealthStatus() *HealthStatus
	PerformHealthCheck(ctx context.Context) *HealthStatus
	GetSerial() string
}

// ConnectionState represents the connection state of a printer
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateError
	StateTimeout
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateError:
		return "error"
	case StateTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// ComponentHealth represents the health status of a component
type ComponentHealth struct {
	Connected bool          `json:"connected"`
	Healthy   bool          `json:"healthy"`
	Latency   time.Duration `json:"latency,omitempty"`
	Error     string        `json:"error,omitempty"`
	LastCheck time.Time     `json:"last_check"`
	LastError string        `json:"last_error,omitempty"`
}

// HealthStatus represents the overall health status of the printer
type HealthStatus struct {
	Timestamp     time.Time       `json:"timestamp"`
	MQTT          ComponentHealth `json:"mqtt"`
	FTP           ComponentHealth `json:"ftp"`
	Camera        ComponentHealth `json:"camera"`
	OverallHealth bool            `json:"overall_health"`
}
