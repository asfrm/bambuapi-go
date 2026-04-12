# BambuSDK-Go Integration Guide (FEED THAT TO AI)

> **Comprehensive API reference and cheat sheet for integrating Bambu Lab 3D printers into Go applications**

**SDK Version:** v1.0.5
**Go Version:** 1.26+
**Last Updated:** 2026-04-09

---

## Features Overview

### Security Enhancements

- **Configurable TLS Verification**: Enable certificate verification for production deployments
- **FTP Path Sanitization**: All file paths are now sanitized to prevent path traversal attacks
- **Input Validation**: AMS/tray ID validation (0-3 range) to prevent invalid commands
- **G-code Validation**: Regex-based validation to prevent malformed commands

### Performance Improvements

- **Streaming FTP Uploads**: Files are streamed directly without buffering in memory
- **Camera Memory Pool**: Reduced GC pressure with buffer pooling and 10MB max image size limit
- **Configurable Command Timeout**: Adjustable MQTT command timeout (default: 5s)
- **Context Support**: `UploadFileWithContext()` and `DownloadFileWithContext()` for cancellable operations

### Core Capabilities

- **MQTT Communication**: Real-time telemetry and commands (TLS:8883)
- **Camera Streaming**: JPEG frame capture with lazy connection pattern (TLS:6000)
- **FTP File Transfer**: Streaming uploads with path sanitization (TLS:990)
- **Fleet Management**: Multi-printer pool with concurrent operations and broadcast
- **AMS Support**: Full AMS hub monitoring (up to 4 units, 4 trays each)
- **Busy State Detection**: Hardware-accurate detection using GcodeState + PrintStatus
- **Health Checks**: Per-component monitoring (MQTT, FTP, Camera)
- **CLI Tool**: 16+ commands for printer management

---

## Table of Contents

- [Quick Reference](#quick-reference)
- [Architecture Overview](#architecture-overview)
- [Installation & Setup](#installation--setup)
- [Connection & Lifecycle](#connection--lifecycle)
- [Fleet Management](#fleet-management)
- [Print Jobs](#print-jobs)
- [Camera Streaming](#camera-streaming)
- [Printer Control](#printer-control)
- [State & Telemetry](#state--telemetry)
- [Busy State Detection](#busy-state-detection)
- [AMS Management](#ams-management)
- [Error Handling](#error-handling)
- [Complete Examples](#complete-examples)
- [Troubleshooting](#troubleshooting)

---

## Quick Reference

### Connection Ports & Protocols

| Service    | Port | Protocol      | Authentication           |
|------------|------|---------------|--------------------------|
| MQTT       | 8883 | TLS + MQTT    | Username: `bblp`, Password: AccessCode |
| FTP        | 990  | Implicit TLS  | Username: `bblp`, Password: AccessCode |
| Camera     | 6000 | Custom TLS TCP| Binary header with credentials |

### Core Methods Cheat Sheet

| Category | Method | Description |
|----------|--------|-------------|
| **Connection** | `Connect()` | Connect to printer (blocks until ready) |
| | `Disconnect()` | Disconnect all services |
| | `ConnectWithContext(ctx)` | Connect with custom timeout |
| **Status** | `GetState()` | Get G-code state (IDLE, RUNNING, etc.) |
| | `GetCurrentState()` | Get detailed print status |
| | `GetPercentage()` | Get print progress (0-100%) |
| | `GetTime()` | Get remaining time (seconds) |
| | `IsBusy()` | Check if printer is busy (hardware-accurate) |
| | `GetActivityDescription()` | Get human-readable activity description |
| **Temperature** | `GetNozzleTemperature()` | Get nozzle temp (°C) |
| | `GetBedTemperature()` | Get bed temp (°C) |
| | `GetChamberTemperature()` | Get chamber temp (°C) |
| | `SetNozzleTemperature(n)` | Set nozzle temp |
| | `SetBedTemperature(n)` | Set bed temp |
| **Fans** | `SetPartFanSpeedInt(n)` | Set part fan (0-255) |
| | `SetAuxFanSpeedInt(n)` | Set aux fan (0-255) |
| | `SetChamberFanSpeedInt(n)` | Set chamber fan (0-255) |
| **Control** | `HomePrinter()` | Home all axes |
| | `PausePrint()` | Pause current print |
| | `ResumePrint()` | Resume paused print |
| | `StopPrint()` | Stop print |
| | `Gcode(cmd, check)` | Send G-code command |
| **Camera** | `CaptureFrame()` | Capture single frame (lazy) |
| | `StartCamera()` | Start continuous stream |
| | `GetCameraFrameBytes()` | Get latest frame as []byte |
| **Files** | `UploadFile(data, name)` | Upload via FTP |
| | `DownloadFile(path)` | Download via FTP |
| | `SubmitPrintJob(...)` | Upload and start print |
| **AMS** | `AMSHub()` | Get AMS hub info |
| | `VTTray()` | Get external spool info |

### State Enums Quick Reference

```go
// GcodeState - High-level printer state
states.GcodeStateIdle       // "IDLE"
states.GcodeStatePrepare    // "PREPARE"
states.GcodeStateRunning    // "RUNNING"
states.GcodeStatePause      // "PAUSE"
states.GcodeStateFinish     // "FINISH"
states.GcodeStateFailed     // "FAILED"

// PrintStatus - Detailed status (selected)
states.PrintStatusIdle                 // 255 - Idle
states.PrintStatusPrinting             // 0 - Printing
states.PrintStatusAutoBedLeveling      // 1 - Leveling
states.PrintStatusHeatbedPreheating    // 2 - Preheating bed
states.PrintStatusSweepingNozzle       // 3 - Sweeping
states.PrintStatusChangingFilament     // 4 - Changing filament
states.PrintStatusCalibratingExtrusion // 5 - Calibrating
states.PrintStatusUserPaused           // 16 - User paused
```

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Application Layer                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │  bambu-cli  │  │   examples  │  │  Your Application Code  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                      Core Package Layer                          │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    printer/printer.go                      │  │
│  │  ┌────────────┐ ┌────────────┐ ┌───────────────────────┐  │  │
│  │  │ MQTTClient │ │ FTPClient  │ │    CameraClient       │  │  │
│  │  └────────────┘ └────────────┘ └───────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                     Communication Layer                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │  mqtt/      │  │   ftp/      │  │      camera/            │  │
│  │  (TLS:8883) │  │  (TLS:990)  │  │      (TLS:6000)         │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                      Support Packages                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │    ams/     │  │  filament/  │  │  states/    │  │ fleet/  │ │
│  │  (AMS Hub)  │  │  (Types)    │  │  (Enums)    │  │ (Pool)  │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### Package Responsibilities

| Package | Responsibility | Key Types |
|---------|---------------|-----------|
| `printer/` | Main client orchestration | `Printer`, `HealthStatus` |
| `mqtt/` | MQTT communication (TLS:8883) | `PrinterMQTTClient` |
| `camera/` | Camera streaming (TLS:6000) | `PrinterCamera` |
| `ftp/` | File transfer (TLS:990) | `PrinterFTPClient` |
| `fleet/` | Multi-printer management | `PrinterPool`, `PrinterConfig` |
| `ams/` | AMS data parsing | `AMSHub`, `AMS` |
| `filament/` | Filament types (40+) | `AMSFilamentSettings`, `FilamentTray` |
| `states/` | State enums | `GcodeState`, `PrintStatus` |
| `printerinfo/` | Printer metadata | `PrinterType`, `NozzleType` |

---

## Installation & Setup

### Import Path

```go
import (
    "github.com/asfrm/bambusdk-go/printer"
    "github.com/asfrm/bambusdk-go/fleet"
    "github.com/asfrm/bambusdk-go/states"
)
```

### Single Printer Instance

```go
// Create printer instance
p := printer.NewPrinter(ipAddress, accessCode, serialNumber)

// Connect (blocks until full state received or timeout)
if err := p.Connect(); err != nil {
    return fmt.Errorf("connection failed: %w", err)
}

// Cleanup on shutdown
defer p.Disconnect()
```

### Printer Pool for Fleet Management

```go
// Create pool
pool := fleet.NewPrinterPool()

// Add printers
pool.AddPrinter(&fleet.PrinterConfig{
    Serial:     "SERIAL123",
    IP:         "192.168.1.100",
    AccessCode: "ABC12345678",
    Name:       "Living Room Printer",
})

// Connect all printers concurrently
results := pool.ConnectAll()
for serial, err := range results {
    if err != nil {
        log.Printf("Failed to connect %s: %v", serial, err)
    }
}

// Cleanup
defer pool.DisconnectAll()
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `BAMBU_IP` | Printer IP address | - |
| `BAMBU_SERIAL` | Printer serial number | - |
| `BAMBU_ACCESS_CODE` | Printer access code (8 digits) | - |
| `BAMBU_DEBUG=1` | Enable debug logging | off |

### Security Configuration

**IMPORTANT:** By default, TLS certificate verification is disabled for backward compatibility. For production deployments, enable certificate verification:

```go
import (
    "time"
    "github.com/asfrm/bambusdk-go/printer"
    "github.com/asfrm/bambusdk-go/mqtt"
)

// Create printer instance
p := printer.NewPrinter(ip, accessCode, serial)

// Configure secure MQTT client
p.MQTTClient = mqtt.NewPrinterMQTTClientWithOptions(
    ip, accessCode, serial, "bblp", 8883, 60, 60, true, false,
    mqtt.WithTLSInsecureSkipVerify(false),  // Enable certificate verification
    mqtt.WithCommandTimeout(10*time.Second), // Increase timeout for slow networks
)

// Connect
if err := p.Connect(); err != nil {
    return err
}
```

### MQTT Client Options

| Option | Description | Default |
|--------|-------------|---------|
| `WithTLSInsecureSkipVerify(false)` | Enable TLS certificate verification | `true` (insecure) |
| `WithCommandTimeout(duration)` | Set MQTT command publish timeout | `5s` |

**Security Warning:** Setting `WithTLSInsecureSkipVerify(false)` enables certificate verification, which protects against man-in-the-middle attacks. However, this requires the printer to have a valid certificate. Use `true` only for testing or when you understand the security implications.

---

## Connection & Lifecycle

### Connect() Behavior

The `Connect()` method is **blocking** and performs:

1. Establishes MQTT connection (TLS on port 8883)
2. Requests full state from printer (`pushall` command)
3. Waits for complete payload (up to 10 seconds)
4. Returns error if timeout or connection fails

```go
// Connection with timeout handling
p := printer.NewPrinter(ip, code, serial)
if err := p.Connect(); err != nil {
    // Handle: network issue, wrong credentials, printer offline
    return err
}
// Printer is now ready for queries
```

### Connection with Custom Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := p.ConnectWithContext(ctx); err != nil {
    return err
}
```

### Real-Time State Updates

For WebSocket backends, use the state update callback system:

```go
// Option 1: Callback function
p.SetStateUpdateCallback(func() {
    // Called on every MQTT message
    state := p.GetState()
    progress := p.GetPercentage()
    // Broadcast via WebSocket
})

// Option 2: Channel-based (non-blocking)
updateChan := p.GetUpdateChannel()
go func() {
    for range updateChan {
        // State updated - fetch latest data
        state := p.GetCurrentState()
        // Send to WebSocket clients
    }
}()
```

### Connection Lifecycle

```go
// Check connection status
if !p.MQTTClientConnected() {
    // Reconnect logic
    if err := p.Connect(); err != nil {
        // Handle reconnection failure
    }
}

// Graceful shutdown
p.Disconnect()  // Stops MQTT and Camera
```

### Connection State Tracking

```go
// Get connection state
state := p.GetConnectionState()
// ConnectionState: StateDisconnected, StateConnecting, StateConnected, StateError, StateTimeout

// Get health status
health := p.GetHealthStatus()
// Returns: HealthStatus{MQTT, FTP, Camera, Timestamp}
```

---

## Fleet Management

### Adding Printers

```go
pool := fleet.NewPrinterPool()

// Add single printer
err := pool.AddPrinter(&fleet.PrinterConfig{
    Serial:     "SERIAL123",
    IP:         "192.168.1.100",
    AccessCode: "ABC12345678",
    Name:       "Printer Name",
})

// Add multiple printers
configs := []*fleet.PrinterConfig{
    {Serial: "S1", IP: "192.168.1.100", AccessCode: "CODE1"},
    {Serial: "S2", IP: "192.168.1.101", AccessCode: "CODE2"},
}
pool.AddPrinters(configs)
```

### Connecting Printers

```go
// Connect single printer
err := pool.ConnectPrinter("SERIAL123")

// Connect all concurrently (recommended)
results := pool.ConnectAll()  // map[string]error
for serial, err := range results {
    if err != nil {
        // Handle individual failures
    }
}

// Check connection status
connected := pool.ListConnectedPrinters()  // []string
status := pool.GetStatus()  // PoolStatus{Total, Connected, Disconnected}
```

### Bulk Operations

```go
// Get info for all printers
infos := pool.GetAllPrinterInfo()  // []*PrinterInfo

// Get specific printer info
info, err := pool.GetPrinterInfo("SERIAL123")

// Iterate over connected printers
pool.ForEachPrinter(func(serial string, p *printer.Printer) {
    // Perform operations on each printer
    p.TurnLightOn()
})

// Broadcast G-code to all connected printers
results := pool.BroadcastGcode("M104 S200", true)  // map[string]bool

// Get status from all printers
statusMap := pool.BroadcastStatus()  // map[string]*PrinterStatus
```

### Pool Status

```go
status := pool.GetStatus()
fmt.Printf("Total: %d, Connected: %d, Disconnected: %d\n",
    status.TotalPrinters,
    status.ConnectedCount,
    status.DisconnectedCount,
)
```

---

## Print Jobs

### SubmitPrintJob Workflow

The `SubmitPrintJob` method handles the complete upload-and-print workflow:

1. **Lazy FTP Connection** - Connects only when needed
2. **File Upload** - Uploads 3MF/Gcode via FTP (implicit TLS, port 990)
3. **FTP Disconnect** - Immediately closes FTP after upload
4. **MQTT Trigger** - Sends `project_file` command to start print

```go
// From io.Reader (e.g., HTTP multipart upload)
uploadedPath, err := p.SubmitPrintJob(
    fileData,           // io.Reader
    "model.3mf",        // filename
    1,                  // plateNumber (1-4)
    true,               // useAMS
    []int{0},           // amsMapping (slot 0)
    true,               // flowCalibration
    "textured_plate",   // bedType
)
if err != nil {
    return err
}
```

### SubmitPrintJobFromFile

```go
// From local file path
uploadedPath, err := p.SubmitPrintJobFromFile(
    "/path/to/model.3mf",  // localPath
    1,                      // plateNumber
    true,                   // useAMS
    []int{0},               // amsMapping
    true,                   // flowCalibration
    "textured_plate",       // bedType
)
```

### Print Job Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `fileData` | `io.Reader` | 3MF or Gcode file data |
| `filename` | `string` | Remote filename on printer |
| `plateNumber` | `int` | Plate number (1-4), defaults to 1 |
| `useAMS` | `bool` | Enable AMS filament system |
| `amsMapping` | `[]int` | AMS slot indices (e.g., `[0]` for first slot) |
| `flowCalibration` | `bool` | Enable flow calibration before print |
| `bedType` | `string` | Bed type: `textured_plate`, `smooth_plate`, etc. |

### Manual Control (Advanced)

```go
// Step 1: Upload file (path is automatically sanitized)
uploadedPath, err := p.UploadFile(fileData, "model.3mf")
if err != nil {
    return err
}

// Step 2: Start print
success := p.StartPrint(
    uploadedPath,     // filename on printer
    1,                // plate number
    true,             // use AMS
    []int{0},         // AMS mapping
    nil,              // skipObjects (optional)
    true,             // flow calibration
)
if !success {
    return fmt.Errorf("failed to start print")
}
```

### FTP Upload with Context (Cancellable)

```go
// Upload with context for cancellation support
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

uploadedPath, err := p.FTPClient.UploadFileWithContext(ctx, fileData, "model.3mf")
if err != nil {
    if ctx.Err() != nil {
        // Upload was cancelled
        return fmt.Errorf("upload cancelled: %w", ctx.Err())
    }
    return err
}
```

### FTP Security Notes

- **Path Sanitization**: All file paths are automatically sanitized to prevent path traversal attacks. Paths like `../../../etc/passwd` are rejected.
- **Streaming Uploads**: Files are streamed directly to the printer without buffering in memory, preventing OOM errors on large files.
- **Depth Limit**: `ListRecursive()` has a maximum depth of 10 levels to prevent stack overflow.

### Print Control

```go
p.PausePrint()    // Pause current print
p.ResumePrint()   // Resume paused print
p.StopPrint()     // Stop print (cannot resume)

// Skip objects during multi-object print
p.SkipObjects([]int{2, 3})  // Skip objects 2 and 3
```

---

## Camera Streaming

### On-Demand Frame Capture

Camera connection is **lazy** - it only connects when `CaptureFrame()` or `GetCameraFrame*()` is called.

```go
// Capture single frame (auto start/stop)
frameBytes, err := p.CaptureFrame()
if err != nil {
    return err
}
// frameBytes is JPEG data - send via WebSocket or save to file

// Custom timeout
frameBytes, err := p.CaptureFrameWithTimeout(15 * time.Second)
```

### Continuous Streaming

```go
// Start camera stream
p.StartCamera()
defer p.StopCamera()

// Poll for frames
ticker := time.NewTicker(500 * time.Millisecond)
for range ticker.C {
    frameBytes, err := p.GetCameraFrameBytes()
    if err != nil {
        continue  // No frame yet
    }
    // Send frameBytes to WebSocket clients
}
```

### Camera Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `GetCameraFrameBytes()` | `[]byte, error` | Get frame as bytes |
| `GetCameraFrame()` | `string, error` | Get frame as base64 string |
| `GetCameraImage()` | `image.Image, error` | Get decoded image |
| `SaveCameraFrame(path)` | `error` | Save to file |
| `CameraIsAlive()` | `bool` | Check if stream is running |

### Camera Performance Notes

- **Memory Limit**: Maximum image size is limited to 10MB to prevent memory exhaustion
- **Buffer Pooling**: Camera uses `sync.Pool` for read buffers to reduce GC pressure
- **Graceful Stop**: `StopCamera()` has a 2-second timeout to prevent hanging if the camera thread is blocked

### WebSocket Integration Pattern

```go
func handleCameraStream(ws *websocket.Conn, p *printer.Printer) {
    p.StartCamera()
    defer p.StopCamera()

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ws.CloseChan():
            return
        case <-ticker.C:
            frameBytes, err := p.GetCameraFrameBytes()
            if err != nil {
                continue
            }
            ws.WriteMessage(websocket.BinaryMessage, frameBytes)
        }
    }
}
```

---

## Printer Control

### Temperature Control

```go
// Set temperatures
p.SetNozzleTemperature(220)           // °C
p.SetBedTemperature(60)               // °C

// With override (bypass safety limits)
p.SetNozzleTemperatureOverride(220, true)
p.SetBedTemperatureOverride(60, true)

// Get temperatures
nozzleTemp := p.GetNozzleTemperature()   // float64
bedTemp := p.GetBedTemperature()         // float64
chamberTemp := p.GetChamberTemperature() // float64
```

### Fan Control

```go
// Set fan speed (0-255)
p.SetPartFanSpeedInt(255)    // Part cooling fan
p.SetAuxFanSpeedInt(128)     // Auxiliary fan
p.SetChamberFanSpeedInt(64)  // Chamber fan

// Or as percentage (0.0-1.0)
p.SetPartFanSpeed(0.5)  // 50% speed

// Get fan speeds
partSpeed := p.GetPartFanSpeed()     // 0-255
auxSpeed := p.GetAuxFanSpeed()       // 0-255
chamberSpeed := p.GetChamberFanSpeed() // 0-255
```

### Light Control

```go
p.TurnLightOn()
p.TurnLightOff()
state := p.GetLightState()  // "on" or "off"
```

### Print Speed

```go
// Set speed level (0-3)
// 0 = Silent, 1 = Standard, 2 = Sport, 3 = Ludicrous
p.SetPrintSpeed(1)

// Get current speed
speed := p.GetPrintSpeed()  // Percentage (e.g., 100)
```

### G-code Commands

```go
// Send single G-code command
success, err := p.Gcode("G28", true)  // true = validate G-code

// Send multiple commands
success, err := p.Gcode([]string{
    "G90",      // Absolute positioning
    "G1 Z10",   // Move Z to 10mm
    "M104 S200", // Set nozzle temp
}, true)

// Common commands
p.HomePrinter()              // G28
p.MoveZAxis(50)              // Move Z to 50mm
```

### Filament Control

```go
// Load/unload filament
p.LoadFilamentSpool()
p.UnloadFilamentSpool()
p.RetryFilamentAction()

// Set AMS filament settings
p.SetFilamentPrinter(
    "FF0000",           // Color (hex)
    "PLA",              // Filament type
    0,                  // AMS ID
    0,                  // Tray ID
)
```

### Calibration

```go
// Run calibration
p.CalibratePrinter(
    true,   // Bed leveling
    true,   // Motor noise calibration
    true,   // Vibration compensation
)
```

### System

```go
p.Reboot()  // Reboot printer
```

---

## State & Telemetry

### Print Status

```go
// Current state
state := p.GetState()           // GcodeState enum
printStatus := p.GetCurrentState() // PrintStatus enum

// Progress
progress := p.GetPercentage()      // 0-100
remainingTime := p.GetTime()       // seconds
currentLayer := p.CurrentLayerNum()
totalLayers := p.TotalLayerNum()

// File info
fileName := p.GetFileName()
subtaskName := p.SubtaskName()
printType := p.PrintType()  // "cloud" or "local"
```

### GcodeState Enum

| Value | String | Description |
|-------|--------|-------------|
| 0 | `IDLE` | Printer is idle |
| 1 | `PREPARE` | Preparing to print |
| 2 | `RUNNING` | Currently printing |
| 3 | `PAUSE` | Print paused |
| 4 | `FINISH` | Print finished |
| 5 | `FAILED` | Print failed |

### PrintStatus Enum (Selected)

| Value | Constant | Description |
|-------|----------|-------------|
| 0 | `PrintStatusPrinting` | Actively printing |
| 1 | `PrintStatusAutoBedLeveling` | Leveling bed |
| 2 | `PrintStatusHeatbedPreheating` | Preheating bed |
| 3 | `PrintStatusSweepingNozzle` | Sweeping nozzle |
| 4 | `PrintStatusChangingFilament` | Changing filament |
| 5 | `PrintStatusCalibratingExtrusion` | Calibrating extrusion |
| 16 | `PrintStatusUserPaused` | User paused |
| 255 | `PrintStatusIdle` | Idle |
| -1 | `PrintStatusUnknown` | Unknown state |

### AMS Status

```go
amsHub := p.AMSHub()
for amsID, ams := range amsHub.AMSHub {
    humidity := ams.Humidity      // %
    temp := ams.Temperature       // °C

    for trayID, tray := range ams.FilamentTrays {
        filamentName := tray.TrayInfoIdx  // e.g., "GF PLA01"
        color := tray.TrayColor           // Hex color
    }
}

// External spool (VT)
vtTray := p.VTTray()
```

### Printer Info

```go
// Nozzle
nozzleType := p.NozzleType()       // enum (stainless_steel, hardened_steel)
nozzleDiameter := p.NozzleDiameter() // mm (e.g., 0.4)

// Network
wifiSignal := p.WifiSignal()  // dBm (e.g., "-45")

info := p.MQTTDump()  // Full raw data dump
```

---

## Busy State Detection

The SDK provides hardware-accurate busy state detection, eliminating the need for arbitrary timeouts in your backend.

### IsBusy() Method

```go
// Check if printer is busy
if p.IsBusy() {
    // Printer is performing a hardware task
    // Do not send conflicting commands (homing, calibration, etc.)
    return fmt.Errorf("printer is busy")
}

// Safe to send commands
p.HomePrinter()
```

**Busy State Logic:**

A printer is considered **busy** (cannot accept conflicting hardware commands) if:

1. **GcodeState** is `RUNNING` or `PREPARE` (actively printing or preparing)
2. **OR PrintStatus** indicates an explicit hardware task (not `IDLE`, not `UNKNOWN`, not `PRINTING`)

This covers all hardware tasks including:
- Bed leveling (`AUTO_BED_LEVELING`)
- Heatbed preheating (`HEATBED_PREHEATING`)
- Filament changes (`CHANGING_FILAMENT`, `FILAMENT_LOADING`, `FILAMENT_UNLOADING`)
- Calibration tasks (`CALIBRATING_EXTRUSION`, `CALIBRATING_MICRO_LIDAR`, `CALIBRATING_MOTOR_NOISE`)
- Homing (`HOMING_TOOLHEAD`)
- Maintenance (`CLEANING_NOZZLE_TIP`, `SWEEPING_XY_MECH_MODE`)
- Error states (all `PAUSED_*` states)

### GetActivityDescription() Method

```go
// Get human-readable activity description
activity := p.GetActivityDescription()
fmt.Printf("Printer is: %s\n", activity)

// Examples:
// - "IDLE" - Printer is idle
// - "RUNNING" - Actively printing
// - "PREPARE" - Preparing to print
// - "CALIBRATING_MICRO_LIDAR" - Calibrating micro lidar
// - "HOMING_TOOLHEAD" - Homing toolhead
// - "HEATBED_PREHEATING" - Preheating heatbed
// - "AUTO_BED_LEVELING" - Auto bed leveling
```

**Return Values:**

| Condition | Returns |
|-----------|---------|
| Printer not busy | `"IDLE"` |
| GcodeState is RUNNING | `"RUNNING"` |
| GcodeState is PREPARE | `"PREPARE"` |
| Hardware task active | PrintStatus string (e.g., `"CALIBRATING_MICRO_LIDAR"`) |

### Thread Safety

Both methods are **thread-safe** and use the existing thread-safe getters `GetState()` and `GetCurrentState()`.

### Example: Safe Command Queue

```go
// Safe command execution with busy check
func executeSafeCommand(p *printer.Printer, cmd func() error) error {
    if p.IsBusy() {
        return fmt.Errorf("printer is busy: %s", p.GetActivityDescription())
    }
    
    if err := cmd(); err != nil {
        return err
    }
    
    return nil
}

// Usage
err := executeSafeCommand(p, func() error {
    p.HomePrinter()
    return nil
})
```

### Example: Frontend Status Display

```go
// WebSocket handler with activity description
func handlePrinterStatus(ws *websocket.Conn, p *printer.Printer) {
    p.SetStateUpdateCallback(func() {
        status := map[string]interface{}{
            "is_busy":           p.IsBusy(),
            "activity":          p.GetActivityDescription(),
            "state":             p.GetState().String(),
            "print_status":      p.GetCurrentState().String(),
            "progress":          p.GetPercentage(),
            "remaining_time":    p.GetTime(),
        }
        ws.WriteJSON(status)
    })
}
```

### State Mapping Reference

| PrintStatus Constant | Value | Activity Description |
|---------------------|-------|---------------------|
| `PrintStatusIdle` | 255 | IDLE (not busy) |
| `PrintStatusUnknown` | -1 | UNKNOWN (not busy) |
| `PrintStatusPrinting` | 0 | PRINTING (handled by GcodeState) |
| `PrintStatusAutoBedLeveling` | 1 | AUTO_BED_LEVELING |
| `PrintStatusHeatbedPreheating` | 2 | HEATBED_PREHEATING |
| `PrintStatusChangingFilament` | 4 | CHANGING_FILAMENT |
| `PrintStatusCalibratingExtrusion` | 8 | CALIBRATING_EXTRUSION |
| `PrintStatusCalibratingMicroLidar` | 12 | CALIBRATING_MICRO_LIDAR |
| `PrintStatusHomingToolhead` | 13 | HOMING_TOOLHEAD |
| `PrintStatusCleaningNozzleTip` | 14 | CLEANING_NOZZLE_TIP |
| `PrintStatusFilamentLoading` | 24 | FILAMENT_LOADING |
| `PrintStatusFilamentUnloading` | 22 | FILAMENT_UNLOADING |
| `PrintStatusCalibratingMotorNoise` | 25 | CALIBRATING_MOTOR_NOISE |

---

## AMS Management

### AMS Hub Structure

```go
type AMSHub struct {
    AMSHub map[int]*AMS  // Key: AMS ID (0-3)
}

type AMS struct {
    FilamentTrays map[int]*filament.FilamentTray  // Key: Tray ID (0-3)
    Humidity      int
    Temperature   float64
}
```

### Accessing AMS Data

```go
amsHub := p.AMSHub()

// Get first AMS unit
ams := amsHub.Get(0)
if ams != nil {
    fmt.Printf("Humidity: %d%%\n", ams.Humidity)
    fmt.Printf("Temperature: %.1f°C\n", ams.Temperature)
    
    // Get first tray
    tray := ams.GetFilamentTray(0)
    if tray != nil {
        fmt.Printf("Filament: %s\n", tray.TrayType)
        fmt.Printf("Color: %s\n", tray.TrayColor)
        fmt.Printf("Min Temp: %d°C\n", tray.NozzleTempMin)
        fmt.Printf("Max Temp: %d°C\n", tray.NozzleTempMax)
    }
}
```

### External Spool (VT Tray)

```go
vtTray := p.VTTray()
if vtTray != nil {
    fmt.Printf("External filament: %s\n", vtTray.TrayType)
}
```

---

## Error Handling

### Connection Errors

```go
p := printer.NewPrinter(ip, code, serial)
if err := p.Connect(); err != nil {
    // Common errors:
    // - "timeout waiting for MQTT connection" - network/firewall
    // - "failed to login to FTP server" - wrong access code
    // - "connection refused" - printer offline
    return err
}
```

### Print Job Errors

```go
_, err := p.SubmitPrintJob(fileData, "model.3mf", 1, true, []int{0}, true, "")
if err != nil {
    // Common errors:
    // - "failed to upload file" - FTP connection failed
    // - "failed to start print job" - MQTT publish failed
    // - "file already exists" - filename collision
    return err
}
```

### Camera Errors

```go
frameBytes, err := p.CaptureFrame()
if err != nil {
    // Common errors:
    // - "timeout waiting for camera frame" - camera busy/offline
    // - "no frame available" - camera not started
    // - "wrong access code or IP" - authentication failed
    return err
}
```

### State Checks

```go
// Check printer state before operations
state := p.GetState()
if state == states.GcodeStateRunning {
    // Cannot home while printing
    return fmt.Errorf("printer is running")
}

// Check connection
if !p.MQTTClientConnected() {
    return fmt.Errorf("not connected")
}
```

---

## Complete Examples

### REST API Handler: Submit Print Job

```go
func handleSubmitPrint(w http.ResponseWriter, r *http.Request) {
    serial := r.URL.Query().Get("serial")
    p, err := pool.GetPrinter(serial)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }

    // Parse multipart form (max 100MB)
    r.ParseMultipartForm(100 << 20)
    file, _, err := r.FormFile("file")
    if err != nil {
        http.Error(w, "missing file", http.StatusBadRequest)
        return
    }
    defer file.Close()

    // Parse options
    plate, _ := strconv.Atoi(r.FormValue("plate"))
    if plate == 0 {
        plate = 1
    }
    useAMS := r.FormValue("ams") != "false"
    bedType := r.FormValue("bed_type")
    if bedType == "" {
        bedType = "textured_plate"
    }

    // Submit print job
    filename := fmt.Sprintf("%d_%s", time.Now().Unix(), file.Filename)
    uploadedPath, err := p.SubmitPrintJob(
        file, filename, plate, useAMS, []int{0}, true, bedType,
    )
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{
        "uploaded_file": uploadedPath,
        "status":        "print_job_submitted",
    })
}
```

### WebSocket Handler: Real-Time Status

```go
func handlePrinterStatus(ws *websocket.Conn, serial string) {
    p, err := pool.GetPrinter(serial)
    if err != nil {
        ws.WriteJSON(map[string]string{"error": err.Error()})
        ws.Close()
        return
    }

    // Set up state update callback
    p.SetStateUpdateCallback(func() {
        status := map[string]interface{}{
            "state":            p.GetState().String(),
            "print_status":     p.GetCurrentState().String(),
            "progress":         p.GetPercentage(),
            "remaining_time":   p.GetTime(),
            "nozzle_temp":      p.GetNozzleTemperature(),
            "bed_temp":         p.GetBedTemperature(),
            "current_layer":    p.CurrentLayerNum(),
            "total_layers":     p.TotalLayerNum(),
            "file_name":        p.GetFileName(),
        }
        ws.WriteJSON(status)
    })

    // Send initial status
    p.RequestFullState()

    // Keep connection alive
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    defer p.SetStateUpdateCallback(nil)

    for {
        select {
        case <-ws.CloseChan():
            return
        case <-ticker.C:
            ws.WriteMessage(websocket.PingMessage, nil)
        }
    }
}
```

### WebSocket Handler: Camera Stream

```go
func handleCameraStream(ws *websocket.Conn, serial string) {
    p, err := pool.GetPrinter(serial)
    if err != nil {
        ws.WriteJSON(map[string]string{"error": err.Error()})
        ws.Close()
        return
    }

    p.StartCamera()
    defer p.StopCamera()

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ws.CloseChan():
            return
        case <-ticker.C:
            frameBytes, err := p.GetCameraFrameBytes()
            if err != nil {
                continue
            }
            ws.WriteMessage(websocket.BinaryMessage, frameBytes)
        }
    }
}
```

### Fleet Status Endpoint

```go
func handleFleetStatus(w http.ResponseWriter, r *http.Request) {
    status := pool.GetStatus()
    infos := pool.GetAllPrinterInfo()

    response := map[string]interface{}{
        "total_printers":    status.TotalPrinters,
        "connected_count":   status.ConnectedCount,
        "printers":          infos,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}
```

### Graceful Shutdown

```go
var (
    pool   *fleet.PrinterPool
    wg     sync.WaitGroup
    done   = make(chan struct{})
)

func main() {
    // Initialize pool
    pool = fleet.NewPrinterPool()
    // ... add printers ...
    pool.ConnectAll()

    // Handle shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

    <-sigChan
    fmt.Println("Shutting down...")

    // Disconnect all printers
    pool.DisconnectAll()

    close(done)
    wg.Wait()
}
```

---

## Troubleshooting

### Connection Issues

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| `timeout waiting for MQTT connection` | Network/firewall blocking port 8883 | Check firewall rules, ensure printer is on same network |
| `failed to login to FTP server` | Wrong access code | Verify 8-digit code from printer Settings > About |
| `connection refused` | Printer offline | Check printer power and network connectivity |
| `certificate unknown` | TLS certificate issue | SDK uses `InsecureSkipVerify: true` by default |

### Camera Not Working

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| `timeout waiting for camera frame` | Camera busy or offline | Wait for printer to be idle, check camera enabled |
| `no frame available` | Camera not started | Call `StartCamera()` before `GetCameraFrame*()` |
| X1 series camera issues | Limited camera support | X1 series may have limited camera functionality |

### FTP Upload Fails

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| `failed to upload file` | Printer not in safe state | Ensure printer is idle or paused |
| `file already exists` | Filename collision | Use unique filenames (e.g., timestamp prefix) |
| `no space left on device` | Printer storage full | Delete old files via `DeleteFile()` |

### Print Job Issues

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| `failed to start print job` | MQTT publish failed | Check MQTT connection, verify file exists |
| Print doesn't start | Wrong plate number | Ensure plate number (1-4) matches printer |
| AMS not loading | Wrong slot mapping | Verify `amsMapping` matches loaded slots |

### State Update Issues

| Symptom | Possible Cause | Solution |
|---------|---------------|----------|
| Callbacks not firing | MQTT disconnected | Check `MQTTClientConnected()`, reconnect if needed |
| Stale data | Printer offline | Reconnect or check network |

---

## Architecture Notes

### Lazy Connections

| Service | Connection Behavior |
|---------|---------------------|
| **MQTT** | Connected on `Connect()`, stays connected until `Disconnect()` |
| **FTP** | Auto-connects on `UploadFile()`, auto-disconnects after operation |
| **Camera** | Auto-starts on `CaptureFrame()`, auto-stops after frame captured |

### Thread Safety

- ✅ Printer methods are thread-safe for concurrent reads
- ⚠️ Use mutex for shared state in your backend
- ✅ PrinterPool is fully thread-safe (uses `sync.RWMutex`)

### Performance

| Metric | Typical Value |
|--------|---------------|
| State updates | Real-time via MQTT push |
| Camera frames | ~50-100KB JPEGs at 2fps |
| FTP uploads | Blocking operation |
| MQTT latency | <100ms on local network |

---

## CLI Tool Reference

The `bambu-cli` tool provides comprehensive command-line access to all printer functions.

### Installation

```bash
go install github.com/asfrm/bambusdk-go/cmd/bambu-cli@latest
```

### Available Commands

| Command | Description |
|---------|-------------|
| `status` | Get printer status (--json, --watch for continuous monitoring) |
| `home` | Home all axes (G28) |
| `temp` | Get/set temperatures (--nozzle, --bed) |
| `light` | Turn light on/off |
| `gcode` | Send raw G-code command |
| `pause` / `resume` / `stop` | Print lifecycle control |
| `fan` | Set fan speed (part/aux/chamber, 0-255) |
| `speed` | Set print speed level (0-3) |
| `calibrate` | Run calibration (--bed, --motor, --vibration) |
| `filament` | Filament control (load/unload/retry) |
| `ams` | Get AMS status and filament info |
| `info` | Get printer info (nozzle, serial, etc.) |
| `firmware` | Show firmware info (deprecated) |
| `reboot` | Reboot printer |
| `camera` | Capture camera frames (-o output, -n count, -i interval) |
| `print` | Upload and start print (3MF/G-code) |
| `ftp-list` | List files on printer |

### Configuration

All commands support the following flags:
- `-ip` or `BAMBU_IP`: Printer IP address
- `-code` or `BAMBU_ACCESS_CODE`: Access code (8 digits)
- `-serial` or `BAMBU_SERIAL`: Printer serial number
- `-debug` or `BAMBU_DEBUG=1`: Enable debug logging

### Example Usage

```bash
# Continuous monitoring
bambu-cli status --watch -ip 192.168.1.200 -code ABC12345 -serial X1C-001

# Start print with AMS
bambu-cli print model.3mf -ip 192.168.1.200 -code ABC12345 -serial X1C-001

# Capture 10 camera frames
bambu-cli camera -o frame.jpg -n 10 -i 1000 -ip 192.168.1.200 -code ABC12345 -serial X1C-001

# List all files on printer
bambu-cli ftp-list -ip 192.168.1.200 -code ABC12345 -serial X1C-001
```

---

## Supported Printer Models

- **P1 Series**: P1S, P1P
- **A1 Series**: A1, A1 Mini
- **X1 Series**: X1C, X1E

---

## Dependencies

```go
github.com/eclipse/paho.mqtt.golang v1.4.3  // MQTT client
github.com/jlaffaye/ftp v0.2.0              // FTP client
golang.org/x/net v0.17.0                    // TLS support
golang.org/x/sync v0.1.0                    // Concurrency utilities
```

---

## CI/CD Pipeline

This project uses GitHub Actions for continuous integration:

- **Lint**: golangci-lint for code quality
- **Build**: Cross-platform compilation
- **Test**: Unit tests with coverage reporting
- **Release**: Automated releases via GoReleaser

Workflows trigger on:
- Push to `main` and `dev` branches
- Pull requests to `main`
- Release tags

---

## Version History

- **v1.0.4** - Current stable (March 2026)
- **v1.0.3** - Feature additions (v1.1 milestone)
- **v1.0.2** - Early development (v0.1.3 milestone)
- **v1.0.1** - Initial project structure

---

## License

MIT License - See LICENSE file for details

## Credits

This Go implementation is inspired by the Python [bambulabs_api](https://github.com/BambuTools/bambulabs_api) project.

Special thanks to the BambuTools community for reverse-engineering the Bambu Lab printer protocol.
