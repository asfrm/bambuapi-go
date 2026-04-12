// Package camera provides camera stream functionality for Bambu Lab printers.
package camera

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	// Debug logging is disabled by default. Set BAMBU_DEBUG=1 to enable.
	debugLog *log.Logger
	errorLog = log.New(os.Stderr, "[CAM ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)
	infoLog  = log.New(os.Stdout, "[CAM INFO] ", log.Ldate|log.Ltime)
)

// CameraClient defines the interface for camera communication with the printer.
type CameraClient interface {
	Start() bool
	Stop()
	IsAlive() bool
	GetFrame() (string, error)
	GetFrameBytes() ([]byte, error)
	WaitForFrame(timeout time.Duration) ([]byte, error)
	WaitForFrameWithStart(timeout time.Duration) ([]byte, error)
}

// Maximum image size to prevent memory exhaustion (10MB)
const maxImageSize = 10 * 1024 * 1024

func init() {
	if os.Getenv("BAMBU_DEBUG") == "1" {
		debugLog = log.New(os.Stdout, "[CAM DEBUG] ", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
	} else {
		debugLog = log.New(io.Discard, "", 0)
	}
}

// PrinterCamera handles camera stream from the printer.
type PrinterCamera struct {
	username   string
	accessCode string
	hostname   string
	port       int

	mu        sync.RWMutex
	thread    chan struct{}
	lastFrame []byte
	alive     bool
	stopChan  chan struct{}

	// TLS configuration
	skipTLSVerify bool
}

// CameraOption is a function type for configuring camera client options.
type CameraOption func(*PrinterCamera)

// WithCameraInsecureSkipVerify sets whether to skip TLS certificate verification.
// WARNING: Setting this to true makes the connection vulnerable to man-in-the-middle attacks.
// Only use for testing or when you understand the security implications.
func WithCameraInsecureSkipVerify(insecure bool) CameraOption {
	return func(c *PrinterCamera) {
		c.skipTLSVerify = insecure
	}
}

// NewPrinterCamera creates a new camera client.
func NewPrinterCamera(hostname, accessCode string, port int, username string, opts ...CameraOption) *PrinterCamera {
	if port == 0 {
		port = 6000
	}
	if username == "" {
		username = "bblp"
	}

	c := &PrinterCamera{
		username:      username,
		accessCode:    accessCode,
		hostname:      hostname,
		port:          port,
		alive:         false,
		stopChan:      make(chan struct{}),
		skipTLSVerify: true, // Default to insecure for backward compatibility
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start starts the camera client.
func (c *PrinterCamera) Start() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.alive {
		return false
	}

	c.alive = true
	c.thread = make(chan struct{})
	go c.retriever()

	infoLog.Println("Starting camera thread")
	return true
}

// Stop stops the camera client.
func (c *PrinterCamera) Stop() {
	c.mu.Lock()
	if !c.alive {
		c.mu.Unlock()
		return
	}
	c.alive = false
	c.mu.Unlock()

	close(c.stopChan)

	// Wait for thread to terminate with timeout to prevent hanging
	if c.thread != nil {
		done := make(chan struct{})
		go func() {
			<-c.thread
			close(done)
		}()

		select {
		case <-done:
			c.thread = nil
		case <-time.After(2 * time.Second):
			errorLog.Println("Camera thread did not terminate cleanly")
		}
	}

	infoLog.Println("Camera client stopped")
}

// IsAlive checks if the camera client is running.
func (c *PrinterCamera) IsAlive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.alive
}

// GetFrame gets the last camera frame as base64 encoded string.
func (c *PrinterCamera) GetFrame() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lastFrame == nil {
		return "", fmt.Errorf("no frame available")
	}

	return base64.StdEncoding.EncodeToString(c.lastFrame), nil
}

// GetFrameBytes gets the last camera frame as bytes.
func (c *PrinterCamera) GetFrameBytes() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lastFrame == nil {
		return nil, fmt.Errorf("no frame available")
	}

	frameCopy := make([]byte, len(c.lastFrame))
	copy(frameCopy, c.lastFrame)
	return frameCopy, nil
}

// WaitForFrame waits for a frame to become available with a timeout.
// It polls every 100ms until a frame is received or the timeout is reached.
// Returns the frame bytes or an error if timeout occurs.
func (c *PrinterCamera) WaitForFrame(timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-c.stopChan:
			return nil, fmt.Errorf("camera stopped while waiting for frame")
		case <-ticker.C:
			c.mu.RLock()
			frame := c.lastFrame
			c.mu.RUnlock()

			if frame != nil {
				frameCopy := make([]byte, len(frame))
				copy(frameCopy, frame)
				return frameCopy, nil
			}
		}
	}

	return nil, fmt.Errorf("timeout waiting for camera frame (%v)", timeout)
}

// WaitForFrameWithStart starts the camera if not running and waits for a frame.
// This is a convenience method that combines Start() and WaitForFrame().
// Returns the frame bytes or an error if the camera fails to start or timeout occurs.
func (c *PrinterCamera) WaitForFrameWithStart(timeout time.Duration) ([]byte, error) {
	// Start camera if not already running
	c.Start()

	// Wait for frame with timeout
	return c.WaitForFrame(timeout)
}

// buildAuthData builds the authentication data for the camera connection.
func (c *PrinterCamera) buildAuthData() []byte {
	authData := make([]byte, 0, 80)

	// Write header fields (little-endian)
	authData = append(authData, byte(0x40), 0x00, 0x00, 0x00) // 0x40
	authData = append(authData, byte(0x00), 0x30, 0x00, 0x00) // 0x3000
	authData = append(authData, byte(0x00), 0x00, 0x00, 0x00) // 0
	authData = append(authData, byte(0x00), 0x00, 0x00, 0x00) // 0

	// Write username (32 bytes, padded with zeros)
	usernameBytes := make([]byte, 32)
	copy(usernameBytes, []byte(c.username))
	authData = append(authData, usernameBytes...)

	// Write access code (32 bytes, padded with zeros)
	accessBytes := make([]byte, 32)
	copy(accessBytes, []byte(c.accessCode))
	authData = append(authData, accessBytes...)

	return authData
}

// retriever is the main camera retrieval loop.
func (c *PrinterCamera) retriever() {
	defer close(c.thread)

	authData := c.buildAuthData()
	connectAttempts := 0

	jpegStart := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	jpegEnd := []byte{0xFF, 0xD9}

	const readChunkSize = 4096

	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.skipTLSVerify,
		MinVersion:         tls.VersionTLS12,
	}

	// Buffer pool for read chunks to reduce GC pressure
	bufPool := sync.Pool{
		New: func() any {
			return make([]byte, readChunkSize)
		},
	}

	for c.alive {
		// Fix: Use net.JoinHostPort for proper IPv6 support
		conn, err := net.Dial("tcp", net.JoinHostPort(c.hostname, strconv.Itoa(c.port)))
		if err != nil {
			errorLog.Printf("Error connecting to camera: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		tlsConn := tls.Client(conn, tlsConfig)
		err = tlsConn.Handshake()
		if err != nil {
			errorLog.Printf("TLS handshake error: %v", err)
			_ = tlsConn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		infoLog.Println("Attempting to connect...")

		_, err = tlsConn.Write(authData)
		if err != nil {
			errorLog.Printf("Error writing auth data: %v", err)
			_ = tlsConn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		var img []byte
		var payloadSize int

		// Set read deadline
		if err := tlsConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			debugLog.Printf("Failed to set read deadline: %v", err)
		}

		for c.alive {
			// Check if we should stop
			select {
			case <-c.stopChan:
				_ = tlsConn.Close()
				return
			default:
			}

			buf := bufPool.Get().([]byte)
			n, err := tlsConn.Read(buf)

			if err != nil {
				bufPool.Put(buf) //nolint:staticcheck // SA6002: performance trade-off is acceptable
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					time.Sleep(1 * time.Second)
					continue
				}
				if err == io.EOF {
					errorLog.Println("Connection closed by server")
					break
				}
				errorLog.Printf("Read error: %v", err)
				time.Sleep(1 * time.Second)
				break
			}

			debugLog.Printf("Read chunk: %d bytes", n)

			switch {
			case img != nil && n > 0:
				debugLog.Println("Appending to image")
				img = append(img, buf[:n]...)

				// Check for maximum image size to prevent memory exhaustion
				if len(img) > maxImageSize {
					errorLog.Printf("Image exceeds maximum size (%d > %d), resetting", len(img), maxImageSize)
					img = nil
					bufPool.Put(buf) //nolint:staticcheck // SA6002
					continue
				}

				if len(img) > payloadSize {
					img = nil
				} else if len(img) == payloadSize {
					// Validate JPEG
					switch {
					case len(img) >= 4 && string(img[:4]) != string(jpegStart):
						img = nil
					case len(img) >= 2 && string(img[len(img)-2:]) != string(jpegEnd):
						img = nil
					default:
						c.mu.Lock()
						c.lastFrame = make([]byte, len(img))
						copy(c.lastFrame, img)
						c.mu.Unlock()
						img = nil
					}
				}
			case n == 16:
				debugLog.Println("Got header")
				connectAttempts = 0
				// Pre-allocate image buffer with expected capacity to reduce reallocations
				img = make([]byte, 0, payloadSize)
				// Payload size is in bytes 0-3 (little-endian, 3 bytes)
				payloadSize = int(binary.LittleEndian.Uint32(append(buf[:3], 0)))
			case n == 0:
				bufPool.Put(buf) //nolint:staticcheck // SA6002
				time.Sleep(5 * time.Second)
				errorLog.Println("Wrong access code or IP")
			default:
				bufPool.Put(buf) //nolint:staticcheck // SA6002
				errorLog.Println("Something bad happened")
				time.Sleep(1 * time.Second)
			}
			bufPool.Put(buf) //nolint:staticcheck // SA6002
		}

		_ = tlsConn.Close()

		if connectAttempts > 10 {
			errorLog.Println("Too many connection attempts, reconnecting...")
			time.Sleep(5 * time.Second)
		}
	}
}
