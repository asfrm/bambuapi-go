BambuAPI-Go Integration Guide
A comprehensive API reference for integrating Bambu Lab printers into the NexusHub backend.

Table of Contents
Installation & Setup
Connection & Events
Fleet Management
Print Jobs
Camera Streaming
Printer Control
State & Telemetry
Error Handling
Complete Examples
Installation & Setup
Import Path
import (
    "github.com/asfrm/bambuapi-go/printer"
    "github.com/asfrm/bambuapi-go/fleet"
)
Single Printer Instance
// Create printer instance
p := printer.NewPrinter(ipAddress, accessCode, serialNumber)

// Connect (blocks until full state received or timeout)
if err := p.Connect(); err != nil {
    return fmt.Errorf("connection failed: %w", err)
}

// Cleanup on shutdown
defer p.Disconnect()
PrinterPool for Fleet Management
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
Connection & Events
Connect() Behavior
The Connect() method is blocking and performs:

Establishes MQTT connection (TLS on port 8883)
Requests full state from printer (pushall command)
Waits for complete payload (up to 10 seconds)
Returns error if timeout or connection fails
// Connection with timeout handling
p := printer.NewPrinter(ip, code, serial)
if err := p.Connect(); err != nil {
    // Handle: network issue, wrong credentials, printer offline
    return err
}
// Printer is now ready for queries
Real-Time State Updates
For WebSocket backends, use the state update callback system:

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
Connection Lifecycle
// Check connection status
if !p.MQTTClientConnected() {
    // Reconnect logic
    if err := p.Connect(); err != nil {
        // Handle reconnection failure
    }
}

// Graceful shutdown
p.Disconnect()  // Stops MQTT and Camera
Fleet Management
Adding Printers
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
Connecting Printers
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
Bulk Operations
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
Print Jobs
SubmitPrintJob Workflow
The SubmitPrintJob method handles the complete upload-and-print workflow:

Lazy FTP Connection - Connects only when needed
File Upload - Uploads 3MF/Gcode via FTP (implicit TLS, port 990)
FTP Disconnect - Immediately closes FTP after upload
MQTT Trigger - Sends project_file command to start print
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
SubmitPrintJobFromFile
// From local file path
uploadedPath, err := p.SubmitPrintJobFromFile(
    "/path/to/model.3mf",  // localPath
    1,                      // plateNumber
    true,                   // useAMS
    []int{0},               // amsMapping
    true,                   // flowCalibration
    "textured_plate",       // bedType
)
Print Job Parameters
Parameter	Type	Description
fileData	io.Reader	3MF or Gcode file data
filename	string	Remote filename on printer
plateNumber	int	Plate number (1-4), defaults to 1
useAMS	bool	Enable AMS filament system
amsMapping	[]int	AMS slot indices (e.g., [0] for first slot)
flowCalibration	bool	Enable flow calibration before print
bedType	string	Bed type: textured_plate, smooth_plate, etc.
Manual Control (Advanced)
// Step 1: Upload file
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
Print Control
p.PausePrint()    // Pause current print
p.ResumePrint()   // Resume paused print
p.StopPrint()     // Stop print (cannot resume)

// Skip objects during multi-object print
p.SkipObjects([]int{2, 3})  // Skip objects 2 and 3
Camera Streaming
On-Demand Frame Capture
Camera connection is lazy - it only connects when CaptureFrame() or GetCameraFrame*() is called.

// Capture single frame (auto start/stop)
frameBytes, err := p.CaptureFrame()
if err != nil {
    return err
}
// frameBytes is JPEG data - send via WebSocket or save to file

// Custom timeout
frameBytes, err := p.CaptureFrameWithTimeout(15 * time.Second)
Continuous Streaming
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
Camera Methods
// Get frame as bytes
frameBytes, err := p.GetCameraFrameBytes()

// Get frame as base64 string
frameBase64, err := p.GetCameraFrame()

// Get decoded image.Image
img, err := p.GetCameraImage()

// Save to file
err := p.SaveCameraFrame("/path/frame.jpg")

// Check camera status
if p.CameraIsAlive() {
    // Camera stream is running
}
WebSocket Integration Pattern
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
Printer Control
Temperature Control
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
Fan Control
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
Light Control
p.TurnLightOn()
p.TurnLightOff()
state := p.GetLightState()  // "on" or "off"
Print Speed
// Set speed level (0-3)
// 0 = Silent, 1 = Standard, 2 = Sport, 3 = Ludicrous
p.SetPrintSpeed(1)

// Get current speed
speed := p.GetPrintSpeed()  // Percentage (e.g., 100)
G-code Commands
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
Filament Control
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
Calibration
// Run calibration
p.CalibratePrinter(
    true,   // Bed leveling
    true,   // Motor noise calibration
    true,   // Vibration compensation
)
System
p.Reboot()  // Reboot printer

// Firmware
newFw := p.NewPrinterFirmware()  // Check for updates
p.UpgradeFirmware(false)         // Upgrade (false = safety check)
State & Telemetry
Print Status
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
State Enums
// GcodeState
states.GcodeStateIdle      // 0
states.GcodeStateRunning   // 1
states.GcodeStatePause     // 2
states.GcodeStateStopped   // 3

// PrintStatus
states.PrintStatusIdle      // -1 or 0
states.PrintStatusRunning   // 1
states.PrintStatusPause     // 2
states.PrintStatusFinished  // 3
AMS Status
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
Printer Info
// Nozzle
nozzleType := p.NozzleType()       // enum
nozzleDiameter := p.NozzleDiameter() // mm (e.g., 0.4)

// Network
wifiSignal := p.WifiSignal()  // dBm (e.g., "-45")

// Firmware
info := p.MQTTDump()  // Full raw data dump
Error Handling
Connection Errors
p := printer.NewPrinter(ip, code, serial)
if err := p.Connect(); err != nil {
    // Common errors:
    // - "timeout waiting for MQTT connection" - network/firewall
    // - "failed to login to FTP server" - wrong access code
    // - "connection refused" - printer offline
    return err
}
Print Job Errors
_, err := p.SubmitPrintJob(fileData, "model.3mf", 1, true, []int{0}, true, "")
if err != nil {
    // Common errors:
    // - "failed to upload file" - FTP connection failed
    // - "failed to start print job" - MQTT publish failed
    // - "file already exists" - filename collision
    return err
}
Camera Errors
frameBytes, err := p.CaptureFrame()
if err != nil {
    // Common errors:
    // - "timeout waiting for camera frame" - camera busy/offline
    // - "no frame available" - camera not started
    // - "wrong access code or IP" - authentication failed
    return err
}
State Checks
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
Complete Examples
REST API Handler: Submit Print Job
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
WebSocket Handler: Real-Time Status
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
WebSocket Handler: Camera Stream
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
Fleet Status Endpoint
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
Graceful Shutdown
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
Architecture Notes
Lazy Connections
MQTT: Connected on Connect(), stays connected until Disconnect()
FTP: Auto-connects on UploadFile(), auto-disconnects after operation
Camera: Auto-starts on CaptureFrame(), auto-stops after frame captured
Thread Safety
Printer methods are thread-safe for concurrent reads
Use mutex for shared state in your backend
PrinterPool is fully thread-safe
Performance
State updates arrive via MQTT push (real-time)
Use SetStateUpdateCallback() instead of polling
Camera frames are ~50-100KB JPEGs
FTP uploads are blocking - use goroutines for large files
SDK Version: 1.0
Last Updated: 2026-03-28
Go Version: 1.21+

