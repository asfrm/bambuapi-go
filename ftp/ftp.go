// Package ftp provides FTP file transfer functionality for Bambu Lab printers.
package ftp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/textproto"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/jlaffaye/ftp"
)

var (
	infoLog = log.New(os.Stdout, "[FTP INFO] ", log.Ldate|log.Ltime)
)

// FTPClient defines the interface for FTP communication with the printer.
type FTPClient interface {
	UploadFile(data io.Reader, filePath string) (string, error)
	UploadFileWithContext(ctx context.Context, data io.Reader, filePath string) (string, error)
	ListDirectory(path string) ([]string, error)
	ListImagesDir() ([]string, error)
	ListCacheDir() ([]string, error)
	ListTimelapseDir() ([]string, error)
	ListLoggerDir() ([]string, error)
	DownloadFile(filePath string) ([]byte, error)
	DownloadFileWithContext(ctx context.Context, filePath string) ([]byte, error)
	DeleteFile(filePath string) error
	GetLastImagePrint() ([]byte, error)
	MakeDir(path string) error
	ChangeDir(path string) error
	GetCurrentDir() (string, error)
	Rename(oldPath, newPath string) error
	GetFileSize(filePath string) (uint64, error)
	Close() error
	IsConnected() bool
	Reconnect() error
	Noop() error
	GetEntry(path string) (*ftp.Entry, error)
	AppendFile(data io.Reader, filePath string) error
	KeepAlive() error
	ListRecursive(path string) ([]string, error)
	FileExists(filePath string) (bool, error)
}

// sanitizePath removes path traversal attempts and ensures safe paths.
// It cleans the path and rejects any paths that attempt to escape the root.
func sanitizePath(filePath string) string {
	// Clean the path to resolve . and ..
	clean := path.Clean(filePath)

	// Reject paths that start with .. or /
	if strings.HasPrefix(clean, "..") || strings.HasPrefix(clean, "/") {
		// Return just the base filename
		return path.Base(filePath)
	}

	return clean
}

// PrinterFTPClient handles FTP communication with the printer.
type PrinterFTPClient struct {
	serverIP   string
	port       int
	user       string
	accessCode string

	mu        sync.Mutex
	ftpClient *ftp.ServerConn
	tlsConfig *tls.Config
}

// FTPOption is a function type for configuring FTP client options.
type FTPOption func(*PrinterFTPClient)

// WithFTPInsecureSkipVerify sets whether to skip TLS certificate verification.
// WARNING: Setting this to true makes the connection vulnerable to man-in-the-middle attacks.
// Only use for testing or when you understand the security implications.
func WithFTPInsecureSkipVerify(insecure bool) FTPOption {
	return func(c *PrinterFTPClient) {
		c.tlsConfig.InsecureSkipVerify = insecure
	}
}

// NewPrinterFTPClient creates a new FTP client.
func NewPrinterFTPClient(serverIP, accessCode string, user string, port int, opts ...FTPOption) *PrinterFTPClient {
	if user == "" {
		user = "bblp"
	}
	if port == 0 {
		port = 990
	}

	c := &PrinterFTPClient{
		serverIP:   serverIP,
		port:       port,
		user:       user,
		accessCode: accessCode,
		tlsConfig: &tls.Config{
			InsecureSkipVerify: true, // Default to insecure for backward compatibility
			MinVersion:         tls.VersionTLS12,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// connect connects to the FTP server with implicit TLS.
func (c *PrinterFTPClient) connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ftpClient != nil {
		return nil
	}

	// Connect with implicit TLS using Dial option
	client, err := ftp.Dial(fmt.Sprintf("%s:%d", c.serverIP, c.port), ftp.DialWithTLS(c.tlsConfig))
	if err != nil {
		// Try explicit TLS as fallback
		client, err = ftp.Dial(fmt.Sprintf("%s:%d", c.serverIP, c.port), ftp.DialWithExplicitTLS(c.tlsConfig))
		if err != nil {
			return fmt.Errorf("failed to connect to FTP server: %w", err)
		}
	}

	err = client.Login(c.user, c.accessCode)
	if err != nil {
		_ = client.Quit()
		return fmt.Errorf("failed to login to FTP server: %w", err)
	}

	c.ftpClient = client
	infoLog.Println("Connected to FTP server")

	return nil
}

// close closes the FTP connection.
func (c *PrinterFTPClient) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ftpClient != nil {
		err := c.ftpClient.Quit()
		c.ftpClient = nil
		if err != nil {
			return fmt.Errorf("failed to close FTP connection: %w", err)
		}
		infoLog.Println("FTP connection closed")
	}
	return nil
}

// withConnection ensures a connection is available and runs the function.
func (c *PrinterFTPClient) withConnection(fn func(*ftp.ServerConn) error) error {
	if err := c.connect(); err != nil {
		return err
	}

	c.mu.Lock()
	client := c.ftpClient
	c.mu.Unlock()

	if client == nil {
		return fmt.Errorf("FTP client not connected")
	}

	return fn(client)
}

// UploadFile uploads a file to the printer.
// The filePath is sanitized to prevent path traversal attacks.
// Data is streamed directly to the FTP server without buffering in memory.
func (c *PrinterFTPClient) UploadFile(data io.Reader, filePath string) (string, error) {
	return c.UploadFileWithContext(context.Background(), data, filePath)
}

// UploadFileWithContext uploads a file to the printer with context support.
// The filePath is sanitized to prevent path traversal attacks.
// Data is streamed directly to the FTP server without buffering in memory.
// The context can be used to cancel the upload operation.
func (c *PrinterFTPClient) UploadFileWithContext(ctx context.Context, data io.Reader, filePath string) (string, error) {
	// Sanitize the file path to prevent path traversal attacks
	safePath := sanitizePath(filePath)

	// Wrap reader with context-aware reader
	wrappedData := &contextReader{reader: data, ctx: ctx}

	err := c.withConnection(func(client *ftp.ServerConn) error {
		infoLog.Printf("Uploading file: %s", safePath)

		// Stream directly to FTP server without buffering in memory
		err := client.Stor(safePath, wrappedData)
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}

		infoLog.Printf("File uploaded successfully: %s", safePath)
		return nil
	})

	if err != nil {
		return "", err
	}

	return safePath, nil
}

// contextReader wraps an io.Reader to check for context cancellation
type contextReader struct {
	reader io.Reader
	ctx    context.Context
}

func (r *contextReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

// ListDirectory lists files in a directory.
func (c *PrinterFTPClient) ListDirectory(path string) ([]string, error) {
	var entries []*ftp.Entry
	err := c.withConnection(func(client *ftp.ServerConn) error {
		var err error
		if path != "" {
			entries, err = client.List(path)
		} else {
			entries, err = client.List("")
		}
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}

	return names, nil
}

// ListImagesDir lists files in the image directory.
func (c *PrinterFTPClient) ListImagesDir() ([]string, error) {
	return c.ListDirectory("image")
}

// ListCacheDir lists files in the cache directory.
func (c *PrinterFTPClient) ListCacheDir() ([]string, error) {
	return c.ListDirectory("cache")
}

// ListTimelapseDir lists files in the timelapse directory.
func (c *PrinterFTPClient) ListTimelapseDir() ([]string, error) {
	return c.ListDirectory("timelapse")
}

// ListLoggerDir lists files in the logger directory.
func (c *PrinterFTPClient) ListLoggerDir() ([]string, error) {
	return c.ListDirectory("logger")
}

// DownloadFile downloads a file from the printer.
func (c *PrinterFTPClient) DownloadFile(filePath string) ([]byte, error) {
	return c.DownloadFileWithContext(context.Background(), filePath)
}

// DownloadFileWithContext downloads a file from the printer with context support.
// The filePath is sanitized to prevent path traversal attacks.
func (c *PrinterFTPClient) DownloadFileWithContext(ctx context.Context, filePath string) ([]byte, error) {
	// Sanitize the file path
	safePath := sanitizePath(filePath)

	var fileData bytes.Buffer

	err := c.withConnection(func(client *ftp.ServerConn) error {
		reader, err := client.Retr(safePath)
		if err != nil {
			return fmt.Errorf("failed to retrieve file: %w", err)
		}
		defer func() { _ = reader.Close() }()

		// Use context-aware copy
		_, err = io.Copy(&fileData, reader)
		return err
	})

	if err != nil {
		return nil, err
	}

	return fileData.Bytes(), nil
}

// DeleteFile deletes a file from the printer.
func (c *PrinterFTPClient) DeleteFile(filePath string) error {
	infoLog.Printf("Deleting file: %s", filePath)

	err := c.withConnection(func(client *ftp.ServerConn) error {
		return client.Delete(filePath)
	})

	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	infoLog.Printf("File deleted: %s", filePath)
	return nil
}

// GetLastImagePrint gets the last image from the image directory.
func (c *PrinterFTPClient) GetLastImagePrint() ([]byte, error) {
	files, err := c.ListImagesDir()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no images found")
	}

	// Get the last file
	lastFile := files[len(files)-1]
	imgPath := fmt.Sprintf("image/%s", lastFile)

	return c.DownloadFile(imgPath)
}

// MakeDir creates a directory on the FTP server.
func (c *PrinterFTPClient) MakeDir(path string) error {
	err := c.withConnection(func(client *ftp.ServerConn) error {
		return client.MakeDir(path)
	})

	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// ChangeDir changes the current directory.
func (c *PrinterFTPClient) ChangeDir(path string) error {
	err := c.withConnection(func(client *ftp.ServerConn) error {
		return client.ChangeDir(path)
	})

	if err != nil {
		return fmt.Errorf("failed to change directory: %w", err)
	}

	return nil
}

// GetCurrentDir gets the current working directory.
func (c *PrinterFTPClient) GetCurrentDir() (string, error) {
	var dir string
	err := c.withConnection(func(client *ftp.ServerConn) error {
		var err error
		dir, err = client.CurrentDir()
		return err
	})

	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return dir, nil
}

// Rename renames a file or directory.
func (c *PrinterFTPClient) Rename(oldPath, newPath string) error {
	err := c.withConnection(func(client *ftp.ServerConn) error {
		return client.Rename(oldPath, newPath)
	})

	if err != nil {
		return fmt.Errorf("failed to rename: %w", err)
	}

	return nil
}

// GetFileSize gets the size of a file.
func (c *PrinterFTPClient) GetFileSize(filePath string) (uint64, error) {
	var size uint64
	err := c.withConnection(func(client *ftp.ServerConn) error {
		fileSize, err := client.FileSize(filePath)
		if err != nil {
			return err
		}
		if fileSize < 0 {
			return fmt.Errorf("invalid file size: %d", fileSize)
		}
		size = uint64(fileSize)
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to get file size: %w", err)
	}

	return size, nil
}

// Close closes the FTP connection.
func (c *PrinterFTPClient) Close() error {
	return c.close()
}

// IsConnected checks if the FTP client is connected.
func (c *PrinterFTPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ftpClient != nil
}

// Reconnect reconnects to the FTP server.
func (c *PrinterFTPClient) Reconnect() error {
	_ = c.close()
	return c.connect()
}

// Noop sends a NOOP command to keep the connection alive.
func (c *PrinterFTPClient) Noop() error {
	err := c.withConnection(func(client *ftp.ServerConn) error {
		return client.NoOp()
	})

	if err != nil {
		return fmt.Errorf("NOOP failed: %w", err)
	}

	return nil
}

// GetEntry gets detailed information about a file or directory.
func (c *PrinterFTPClient) GetEntry(path string) (*ftp.Entry, error) {
	var entry *ftp.Entry
	err := c.withConnection(func(client *ftp.ServerConn) error {
		entries, err := client.List(path)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("entry not found: %s", path)
		}
		entry = entries[0]
		return nil
	})

	if err != nil {
		return nil, err
	}

	return entry, nil
}

// AppendFile appends data to an existing file.
// Note: This method reads both the existing file and new data into memory.
// For large files, consider downloading, appending locally, and re-uploading.
func (c *PrinterFTPClient) AppendFile(data io.Reader, filePath string) error {
	// Sanitize the file path
	safePath := sanitizePath(filePath)

	err := c.withConnection(func(client *ftp.ServerConn) error {
		// Read new data
		fileData, err := io.ReadAll(data)
		if err != nil {
			return fmt.Errorf("failed to read file data: %w", err)
		}

		// Get current file content
		currentData, err := c.DownloadFile(safePath)
		if err != nil && !strings.Contains(err.Error(), "failed") {
			// File doesn't exist, just upload
			return client.Stor(safePath, bytes.NewReader(fileData))
		}

		// Append new data
		currentData = append(currentData, fileData...)
		return client.Stor(safePath, bytes.NewReader(currentData))
	})

	return err
}

// KeepAlive keeps the connection alive.
func (c *PrinterFTPClient) KeepAlive() error {
	return c.Noop()
}

// ListRecursive lists all files recursively.
// Uses a maximum depth of 10 to prevent stack overflow on deep directory structures.
func (c *PrinterFTPClient) ListRecursive(path string) ([]string, error) {
	return c.listRecursiveWithDepth(path, 10)
}

// listRecursiveWithDepth lists all files recursively with a maximum depth limit.
func (c *PrinterFTPClient) listRecursiveWithDepth(path string, maxDepth int) ([]string, error) {
	if maxDepth < 0 {
		return nil, fmt.Errorf("maximum recursion depth exceeded")
	}

	var allFiles []string

	err := c.withConnection(func(client *ftp.ServerConn) error {
		entries, err := client.List(path)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.Type == ftp.EntryTypeFolder {
				if entry.Name != "." && entry.Name != ".." {
					subPath := fmt.Sprintf("%s/%s", path, entry.Name)
					subFiles, err := c.listRecursiveWithDepth(subPath, maxDepth-1)
					if err != nil {
						return err
					}
					allFiles = append(allFiles, subFiles...)
				}
			} else {
				filePath := fmt.Sprintf("%s/%s", path, entry.Name)
				allFiles = append(allFiles, filePath)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return allFiles, nil
}

// FileExists checks if a file exists.
func (c *PrinterFTPClient) FileExists(filePath string) (bool, error) {
	_, err := c.GetFileSize(filePath)
	if err != nil {
		if protoErr, ok := err.(*textproto.Error); ok {
			if protoErr.Code == 550 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}
