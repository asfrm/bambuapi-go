[![Go Report Card](https://goreportcard.com/badge/github.com/asfrm/bambusdk-go)](https://goreportcard.com/report/github.com/asfrm/bambusdk-go)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Go Reference](https://pkg.go.dev/badge/github.com/asfrm/bambusdk-go.svg)](https://pkg.go.dev/github.com/asfrm/bambusdk-go)

bambusdk-go

Native Go implementation of the Bambu Lab 3D Printer API.

> **Note:** This is a native Go implementation of the Bambu Lab printer API for controlling Bambu Lab 3D printers.

## Current Version: v1.0.6

latest changes - remove unsupported Firmware-related funcs

### Supported Printers
- **P1 Series**: P1P, P1S - not tested yet
- **A1 Series**: A1, A1 Mini - ~85% coverage - camera stream not working.
- **X1 Series**: X1C, X1E - not tested yet

### Security
- **Configurable TLS Verification** - Enable certificate verification for production use
- **FTP Path Sanitization** - Automatic protection against path traversal attacks
- **Input Validation** - AMS/tray ID range validation (0-3)

### Performance
- **Streaming FTP Uploads** - No more memory buffering for large files
- **Camera Memory Pool** - Reduced GC pressure with buffer pooling
- **Configurable Timeouts** - Adjustable MQTT command timeout

### Bug Fixes
- Fixed race conditions in `Connect()` and `ConnectWithContext()`
- Fixed goroutine leaks in camera and MQTT stop procedures
- Fixed unsafe type assertions and G-code validation regex

## Features

- **CLI Tool**: Comprehensive command-line interface with 16+ commands
- **MQTT Control**: Full control over printer via MQTT protocol
- **Camera Stream**: Real-time camera frame retrieval with memory pooling
- **FTP File Transfer**: Streaming upload/download with context cancellation support
- **Fleet Management**: Multi-printer pool with broadcast operations
- **AMS Support**: Monitor and control AMS (Automated Material System)
- **Temperature Control**: Bed and nozzle temperature management
- **Fan Control**: Part, auxiliary, and chamber fan speed control
- **Print Management**: Start, stop, pause, resume prints with full Bambu Studio compatibility
- **G-code Commands**: Send custom G-code commands with validation
- **Busy State Detection**: Hardware-accurate busy detection using GcodeState and PrintStatus
- **Health Checks**: Per-component health monitoring (MQTT, FTP, Camera)

## Installation

```bash
go get github.com/asfrm/bambusdk-go
```

### CLI Tool

Install the CLI tool:

```bash
go install github.com/asfrm/bambusdk-go/cmd/bambu-cli@latest
```

Usage examples:

```bash
# Check printer status
bambu-cli status -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL

# Watch printer status continuously
bambu-cli status --watch -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL

# Capture camera frame
bambu-cli camera -o frame.jpg -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL

# Start a print job
bambu-cli print model.3mf -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL

# Get AMS status
bambu-cli ams -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL

# List files on printer
bambu-cli ftp-list -ip 192.168.1.200 -code YOUR_CODE -serial YOUR_SERIAL
```

## Quick Start

```go
package main

import (
    "fmt"
    "time"

    "github.com/asfrm/bambusdk-go/printer"
)

func main() {
    // Create printer instance
    p := printer.NewPrinter("192.168.1.200", "YOUR_ACCESS_CODE", "YOUR_SERIAL")

    // Connect to printer
    err := p.Connect()
    if err != nil {
        panic(err)
    }
    defer p.Disconnect()

    // Wait for connection
    time.Sleep(2 * time.Second)

    // Get printer status
    fmt.Printf("State: %s\n", p.GetState())
    fmt.Printf("Bed Temp: %.1f°C\n", p.GetBedTemperature())
    fmt.Printf("Nozzle Temp: %.1f°C\n", p.GetNozzleTemperature())

    // Control printer
    p.TurnLightOn()
    p.HomePrinter()
}
```

## Secure Configuration (Production)

By default, TLS certificate verification is disabled for backward compatibility. For production deployments:

```go
package main

import (
    "time"
    "github.com/asfrm/bambusdk-go/printer"
    "github.com/asfrm/bambusdk-go/mqtt"
)

func main() {
    p := printer.NewPrinter("192.168.1.200", "YOUR_ACCESS_CODE", "YOUR_SERIAL")
    
    // Configure secure MQTT client
    p.MQTTClient = mqtt.NewPrinterMQTTClientWithOptions(
        "192.168.1.200", "YOUR_ACCESS_CODE", "YOUR_SERIAL", "bblp", 8883, 60, 60, true, false,
        mqtt.WithTLSInsecureSkipVerify(false),   // Enable cert verification
        mqtt.WithCommandTimeout(10*time.Second), // Increase timeout
    )
    
    if err := p.Connect(); err != nil {
        panic(err)
    }
    defer p.Disconnect()
    
    // Use printer...
}
```

## Usage Examples

### Monitor Print Progress

```go
p := printer.NewPrinter(ip, accessCode, serial)
p.Connect()
defer p.Disconnect()

for {
    percentage := p.GetPercentage()
    remaining := p.GetTime()
    state := p.GetState()

    fmt.Printf("Progress: %d%%, Remaining: %v, State: %s\n",
        percentage, remaining, state)

    time.Sleep(5 * time.Second)
}
```

### Send G-code Commands

```go
// Home printer
p.HomePrinter()

// Set bed temperature
p.SetBedTemperature(60)

// Set nozzle temperature
p.SetNozzleTemperature(200)

// Custom G-code
p.Gcode("G1 X100 Y100 F3000", true)
```

### Control Fans

```go
// Set part fan speed (0-255)
p.SetPartFanSpeedInt(128)

// Or use percentage (0.0-1.0)
p.SetPartFanSpeed(0.5)

// Control auxiliary and chamber fans
p.SetAuxFanSpeedInt(200)
p.SetChamberFanSpeedInt(100)
```

### AMS Management

```go
// Get AMS hub info
amsHub := p.AMSHub()

// Access individual AMS units
ams := amsHub.Get(0)
if ams != nil {
    fmt.Printf("Humidity: %d%%\n", ams.Humidity)
    fmt.Printf("Temperature: %.1f°C\n", ams.Temperature)

    // Get filament tray info
    tray := ams.GetFilamentTray(0)
    if tray != nil {
        fmt.Printf("Filament: %s\n", tray.TrayType)
    }
}
```

### Camera Access

```go
// Get camera frame as base64
frame, err := p.GetCameraFrame()
if err != nil {
    log.Fatal(err)
}

// Get camera frame as image
img, err := p.GetCameraImage()
if err != nil {
    log.Fatal(err)
}
```

### File Operations (FTP)

```go
// Upload file (path is automatically sanitized for security)
file, _ := os.Open("print.gcode")
p.UploadFile(file, "my_print.gcode")

// Upload with context (cancellable)
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()
p.FTPClient.UploadFileWithContext(ctx, file, "large_print.3mf")

// Start print
p.StartPrint("my_print.gcode", 1, true, []int{0}, nil, true)

// Download file
data, err := p.DownloadFile("image/last_print.jpg")

// Delete file
p.DeleteFile("cache/temp.gcode")
```

### Print Control

```go
// Start print
p.StartPrint("file.gcode", 1, true, []int{0}, nil, true)

// Pause print
p.PausePrint()

// Resume print
p.ResumePrint()

// Stop print
p.StopPrint()

// Skip objects during print
p.SkipObjects([]int{1, 3, 5})
```

### Temperature Control

```go
// Set bed temperature
p.SetBedTemperature(60)

// Set nozzle temperature
p.SetNozzleTemperature(210)

// Low temperature warning (use override if needed)
p.SetBedTemperatureOverride(30, true)
```

### Calibration

```go
// Full calibration
p.CalibratePrinter(true, true, true)

// Bed leveling only
p.CalibratePrinter(true, false, false)
```

### Filament Management

```go
// Load filament
p.LoadFilamentSpool()

// Unload filament
p.UnloadFilamentSpool()

// Set filament settings
p.SetFilamentPrinter("FF0000", "BAMBU_PLA", 255, 254)

// Retry filament action
p.RetryFilamentAction()
```

## API Reference

### Printer Methods

| Method | Description |
|--------|-------------|
| `Connect()` | Connect to printer (MQTT + Camera) |
| `Disconnect()` | Disconnect from printer |
| `GetState()` | Get printer G-code state |
| `GetCurrentState()` | Get detailed printer status |
| `GetPercentage()` | Get print completion percentage |
| `GetTime()` | Get remaining print time |
| `GetBedTemperature()` | Get bed temperature |
| `GetNozzleTemperature()` | Get nozzle temperature |
| `GetChamberTemperature()` | Get chamber temperature |
| `GetPrintSpeed()` | Get print speed percentage |
| `GetFileName()` | Get current print file name |
| `TurnLightOn()` | Turn on printer light |
| `TurnLightOff()` | Turn off printer light |
| `HomePrinter()` | Home all axes |
| `MoveZAxis(height)` | Move Z-axis to height |
| `PausePrint()` | Pause current print |
| `ResumePrint()` | Resume paused print |
| `StopPrint()` | Stop current print |
| `Gcode(cmd, check)` | Send G-code command |
| `UploadFile(data, name)` | Upload file via FTP |
| `StartPrint(...)` | Start printing a file |
| `GetCameraFrame()` | Get camera frame (base64) |
| `GetCameraImage()` | Get camera frame (image) |
| `AMSHub()` | Get AMS hub information |
| `Reboot()` | Reboot printer |

## Printer States

### GcodeState

- `IDLE` - Printer is idle
- `PREPARE` - Preparing to print
- `RUNNING` - Currently printing
- `PAUSE` - Print paused
- `FINISH` - Print finished
- `FAILED` - Print failed

### PrintStatus

Detailed status including:
- `PRINTING` - Actively printing
- `AUTO_BED_LEVELING` - Leveling bed
- `HEATBED_PREHEATING` - Preheating bed
- `CHANGING_FILAMENT` - Changing filament
- `CALIBRATING_EXTRUSION` - Calibrating extrusion
- And many more...

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `BAMBU_IP` | Printer IP address | - |
| `BAMBU_SERIAL` | Printer serial number | - |
| `BAMBU_ACCESS_CODE` | Printer access code | - |

## Troubleshooting

### Connection Issues

1. Ensure printer is on the same network
2. Verify access code is correct (8 digits)
3. Check firewall settings (ports 8883, 6000, 990)
4. **TLS Issues**: If using `WithTLSInsecureSkipVerify(false)`, ensure printer has valid certificate

### Camera Not Working

- X1 series: Camera support may be limited - not tested yet.
- Check access code permissions
- **Memory Limit**: Images larger than 10MB are rejected to prevent OOM

### FTP Upload Fails

- Verify printer is idle or in safe state
- Check available storage on printer
- Ensure file name is valid
- **Path Security**: Paths with `..` or absolute paths are automatically sanitized
- **Large Files**: Use `UploadFileWithContext()` for cancellable uploads of large files

### Command Timeouts

- Default MQTT command timeout is 5 seconds
- Increase with `mqtt.WithCommandTimeout(10*time.Second)` if on slow network
- Commands may timeout if printer is busy or in error state

## Project Structure

```
bambusdk-go/
├── cmd/
│   └── bambu-cli/      # CLI tool (16+ commands)
├── printer/            # Main printer client
├── mqtt/               # MQTT communication (TLS:8883)
├── camera/             # Camera stream (TLS:6000)
├── ftp/                # FTP file transfer (TLS:990)
├── ams/                # AMS (Automated Material System)
├── filament/           # Filament types and settings (50+ types)
├── printerinfo/        # Printer information types
├── states/             # Printer state types (GcodeState, PrintStatus)
├── fleet/              # Multi-printer fleet management
├── internal/
│   └── util/           # Type conversion utilities
├── examples/           # Example applications
└── .github/
    └── workflows/      # CI/CD pipeline
```

## License

This project is licensed under the GNU GPL v3.0. See the LICENSE file for details.

## Credits

This Go implementation is inspired by the Python [bambulabs_api](https://github.com/BambuTools/bambulabs_api) project.

Special thanks to the BambuTools community for reverse-engineering the Bambu Lab printer protocol.

Like it? Give it a ⭐.