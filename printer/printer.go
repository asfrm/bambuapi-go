// Package printer provides the main client for connecting to and controlling Bambu Lab 3D printers.
package printer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/asfrm/bambusdk-go/ams"
	"github.com/asfrm/bambusdk-go/camera"
	"github.com/asfrm/bambusdk-go/filament"
	"github.com/asfrm/bambusdk-go/ftp"
	"github.com/asfrm/bambusdk-go/mqtt"
	"github.com/asfrm/bambusdk-go/printerinfo"
	"github.com/asfrm/bambusdk-go/sdk"
	"github.com/asfrm/bambusdk-go/states"
)

// Compile-time interface assertions
var (
	_ sdk.Printer         = (*Printer)(nil)
	_ mqtt.MQTTClient     = (*mqtt.PrinterMQTTClient)(nil)
	_ ftp.FTPClient       = (*ftp.PrinterFTPClient)(nil)
	_ camera.CameraClient = (*camera.PrinterCamera)(nil)
)

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

// HealthStatus represents the health status of all printer components
type HealthStatus struct {
	MQTT      ComponentHealth `json:"mqtt"`
	FTP       ComponentHealth `json:"ftp"`
	Camera    ComponentHealth `json:"camera"`
	Timestamp time.Time       `json:"timestamp"`
}

// ComponentHealth represents the health of a single component
type ComponentHealth struct {
	Connected bool          `json:"connected"`
	Latency   time.Duration `json:"latency_ms"`
	LastError string        `json:"last_error,omitempty"`
	LastCheck time.Time     `json:"last_check"`
}

// Printer is the main client for connecting to and controlling a Bambu Lab 3D printer.
type Printer struct {
	IPAddress  string
	AccessCode string
	Serial     string

	MQTTClient   *mqtt.PrinterMQTTClient
	CameraClient *camera.PrinterCamera
	FTPClient    *ftp.PrinterFTPClient

	// Connection state tracking
	stateMu          sync.RWMutex
	lastHealthCheck  time.Time
	lastHealthStatus *sdk.HealthStatus
}

// NewPrinter creates a new Printer instance.
func NewPrinter(ipAddress, accessCode, serial string) *Printer {
	return &Printer{
		IPAddress:    ipAddress,
		AccessCode:   accessCode,
		Serial:       serial,
		MQTTClient:   mqtt.NewPrinterMQTTClient(ipAddress, accessCode, serial, "bblp", 8883, 60, 60, true, false),
		CameraClient: camera.NewPrinterCamera(ipAddress, accessCode, 6000, "bblp"),
		FTPClient:    ftp.NewPrinterFTPClient(ipAddress, accessCode, "bblp", 990),
	}
}

// Connect connects to the printer (MQTT and Camera).
// It automatically requests full state and waits for the first complete payload.
// Returns an error if connection fails or times out waiting for data (10s timeout).
func (p *Printer) Connect() error {
	// Disable aggressive mode to avoid blocking on info requests
	p.MQTTClient.SetPushallAggressive(false)

	// Start MQTT client
	if err := p.MQTTClient.Start(); err != nil {
		return fmt.Errorf("failed to start MQTT client: %w", err)
	}

	// Wait for MQTT connection with timeout
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

connectionLoop:
	for {
		select {
		case <-timeout:
			p.MQTTClient.Stop()
			return fmt.Errorf("timeout waiting for MQTT connection")
		case <-ticker.C:
			if p.MQTTClient.IsConnected() {
				break connectionLoop
			}
		}
	}

	// Wait for initial data payload
	time.Sleep(300 * time.Millisecond)

	// Request full state from printer
	p.MQTTClient.RequestFullState()

	// Wait for full state to arrive (check for key fields)
	dataTimeout := time.After(5 * time.Second)
	dataTicker := time.NewTicker(100 * time.Millisecond)
	defer dataTicker.Stop()

	for {
		select {
		case <-dataTimeout:
			// Timeout but continue with whatever data we have
			return nil
		case <-dataTicker.C:
			dump := p.MQTTClient.Dump()
			if printData, ok := dump["print"].(map[string]any); ok {
				// Check for key fields that indicate full state
				if _, hasBed := printData["bed_temper"]; hasBed {
					if _, hasAms := printData["ams"]; hasAms {
						return nil // Full state received
					}
				}
			}
		}
	}
}

// Disconnect disconnects from the printer.
func (p *Printer) Disconnect() {
	p.MQTTClient.Stop()
	p.CameraClient.Stop()
}

// DisconnectWithContext disconnects from the printer with context support.
// This allows cancellation of the disconnect operation if it hangs.
func (p *Printer) DisconnectWithContext(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		p.MQTTClient.Stop()
		p.CameraClient.Stop()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("disconnect cancelled: %w", ctx.Err())
	case <-done:
		return nil
	}
}

// ConnectWithContext connects to the printer with context support.
// This allows cancellation and proper timeout handling.
func (p *Printer) ConnectWithContext(ctx context.Context) error {
	// Disable aggressive mode to avoid blocking on info requests
	p.MQTTClient.SetPushallAggressive(false)

	// Start MQTT client
	if err := p.MQTTClient.Start(); err != nil {
		return fmt.Errorf("failed to start MQTT client: %w", err)
	}

	// Wait for MQTT connection with context timeout
	connectionTicker := time.NewTicker(50 * time.Millisecond)
	defer connectionTicker.Stop()

connectionLoop:
	for {
		select {
		case <-ctx.Done():
			p.MQTTClient.Stop()
			return fmt.Errorf("connection cancelled: %w", ctx.Err())
		case <-connectionTicker.C:
			if p.MQTTClient.IsConnected() {
				break connectionLoop
			}
		}
	}

	// Wait for initial data payload with context
	select {
	case <-ctx.Done():
		p.MQTTClient.Stop()
		return fmt.Errorf("initial data wait cancelled: %w", ctx.Err())
	case <-time.After(300 * time.Millisecond):
	}

	// Request full state from printer
	p.MQTTClient.RequestFullState()

	// Wait for full state to arrive with context timeout
	dataTicker := time.NewTicker(100 * time.Millisecond)
	defer dataTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Timeout but continue with whatever data we have
			return nil
		case <-dataTicker.C:
			dump := p.MQTTClient.Dump()
			if printData, ok := dump["print"].(map[string]any); ok {
				// Check for key fields that indicate full state
				if _, hasBed := printData["bed_temper"]; hasBed {
					if _, hasAms := printData["ams"]; hasAms {
						return nil // Full state received
					}
				}
			}
		}
	}
}

// SetStateUpdateCallback sets a callback function to be called on each state update.
// The callback is triggered whenever new MQTT data is received and parsed.
// Only one callback can be registered at a time.
func (p *Printer) SetStateUpdateCallback(callback func()) {
	p.MQTTClient.SetStateUpdateCallback(callback)
}

// GetUpdateChannel returns a channel that receives a signal on each state update.
// External applications can use this to react to real-time printer updates.
// The caller is responsible for reading from the channel to prevent blocking.
func (p *Printer) GetUpdateChannel() <-chan struct{} {
	return p.MQTTClient.GetUpdateChannel()
}

// CameraClientAlive checks if the camera client is running.
func (p *Printer) CameraClientAlive() bool {
	return p.CameraClient.IsAlive()
}

// MQTTClientConnected checks if the MQTT client is connected.
func (p *Printer) MQTTClientConnected() bool {
	return p.MQTTClient.IsConnected()
}

// MQTTClientReady checks if the MQTT client is ready.
func (p *Printer) MQTTClientReady() bool {
	return p.MQTTClient.Ready()
}

// Ping performs a health check on the printer by verifying MQTT connection
// and optionally sending a ping command. Returns error if printer is unreachable.
func (p *Printer) Ping(ctx context.Context) error {
	// Check MQTT connection first
	if !p.MQTTClient.IsConnected() {
		return fmt.Errorf("MQTT not connected")
	}

	// Try to request state update as a ping
	// This is a lightweight operation that should respond quickly
	p.MQTTClient.RequestFullState()

	// Wait for response with context timeout
	select {
	case <-ctx.Done():
		return fmt.Errorf("ping timeout: %w", ctx.Err())
	case <-time.After(500 * time.Millisecond):
		// Give it a moment to respond
		if p.MQTTClient.Ready() {
			return nil
		}
		return fmt.Errorf("printer not responding to ping")
	}
}

// GetConnectionState returns the current connection state of the printer
func (p *Printer) GetConnectionState() sdk.ConnectionState {
	if !p.MQTTClient.IsConnected() {
		return sdk.StateDisconnected
	}
	if !p.MQTTClient.Ready() {
		return sdk.StateConnecting
	}
	return sdk.StateConnected
}

// GetHealthStatus returns cached health status of all printer components
func (p *Printer) GetHealthStatus() *sdk.HealthStatus {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()

	if p.lastHealthStatus == nil {
		// Return current state if no health check has been performed
		return &sdk.HealthStatus{
			Timestamp: time.Now(),
			MQTT: sdk.ComponentHealth{
				Connected: p.MQTTClient.IsConnected(),
				LastCheck: time.Now(),
			},
			FTP: sdk.ComponentHealth{
				Connected: p.FTPClient != nil,
				LastCheck: time.Now(),
			},
			Camera: sdk.ComponentHealth{
				Connected: p.CameraClient.IsAlive(),
				LastCheck: time.Now(),
			},
		}
	}

	// Return cached status
	status := *p.lastHealthStatus
	return &status
}

// updateHealthStatus updates the cached health status
func (p *Printer) updateHealthStatus(status *sdk.HealthStatus) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.lastHealthStatus = status
	p.lastHealthCheck = time.Now()
}

// PerformHealthCheck performs a comprehensive health check on all components
// PerformHealthCheck performs a health check on all printer components
// and caches the result. Use GetHealthStatus() to retrieve cached results.
func (p *Printer) PerformHealthCheck(ctx context.Context) *sdk.HealthStatus {
	status := &sdk.HealthStatus{
		Timestamp: time.Now(),
	}

	// Check MQTT
	mqttStart := time.Now()
	status.MQTT = sdk.ComponentHealth{
		Connected: p.MQTTClient.IsConnected(),
		Latency:   time.Since(mqttStart),
		LastCheck: time.Now(),
	}
	if !status.MQTT.Connected {
		status.MQTT.LastError = "not connected"
	}

	// Check FTP (lightweight check - just verify client exists)
	status.FTP = sdk.ComponentHealth{
		Connected: p.FTPClient != nil,
		LastCheck: time.Now(),
	}

	// Check Camera
	status.Camera = sdk.ComponentHealth{
		Connected: p.CameraClient.IsAlive(),
		LastCheck: time.Now(),
	}
	if !status.Camera.Connected {
		status.Camera.LastError = "camera not running"
	}

	// Cache the result
	p.updateHealthStatus(status)

	return status
}

// SetMQTTAggressiveMode sets whether to send aggressive pushall/info requests on connect.
// Set to false for CLI usage to avoid blocking.
func (p *Printer) SetMQTTAggressiveMode(enabled bool) {
	p.MQTTClient.SetPushallAggressive(enabled)
}

// boolToError converts a boolean return value to an error.
// Returns nil if success (true), or a generic error if failure (false).
func boolToError(success bool) error {
	if !success {
		return fmt.Errorf("operation failed")
	}
	return nil
}

// RequestFullState requests a full state update from the printer.
func (p *Printer) RequestFullState() error {
	return boolToError(p.MQTTClient.RequestFullState())
}

// CurrentLayerNum gets the current layer number.
func (p *Printer) CurrentLayerNum() int {
	return p.MQTTClient.CurrentLayerNum()
}

// TotalLayerNum gets the total layer number.
func (p *Printer) TotalLayerNum() int {
	return p.MQTTClient.TotalLayerNum()
}

// CameraStart starts the camera client.
func (p *Printer) CameraStart() error {
	if !p.CameraClient.Start() {
		return fmt.Errorf("failed to start camera")
	}
	return nil
}

// MQTTStart starts the MQTT client.
func (p *Printer) MQTTStart() error {
	return p.MQTTClient.Start()
}

// MQTTStop stops the MQTT client.
func (p *Printer) MQTTStop() {
	p.MQTTClient.Stop()
}

// CameraStop stops the camera client.
func (p *Printer) CameraStop() {
	p.CameraClient.Stop()
}

// GetTime gets the remaining print time in seconds.
func (p *Printer) GetTime() int {
	return p.MQTTClient.GetRemainingTime()
}

// MQTTDump gets the full MQTT data dump.
func (p *Printer) MQTTDump() map[string]any {
	return p.MQTTClient.Dump()
}

// GetPercentage gets the print completion percentage.
func (p *Printer) GetPercentage() int {
	return p.MQTTClient.GetLastPrintPercentage()
}

// GetState gets the printer G-code state.
func (p *Printer) GetState() states.GcodeState {
	return p.MQTTClient.GetPrinterState()
}

// GetPrintSpeed gets the print speed.
func (p *Printer) GetPrintSpeed() int {
	return p.MQTTClient.GetPrintSpeed()
}

// GetBedTemperature gets the bed temperature.
func (p *Printer) GetBedTemperature() float64 {
	return p.MQTTClient.GetBedTemperature()
}

// GetNozzleTemperature gets the nozzle temperature.
func (p *Printer) GetNozzleTemperature() float64 {
	return p.MQTTClient.GetNozzleTemperature()
}

// GetChamberTemperature gets the chamber temperature.
func (p *Printer) GetChamberTemperature() float64 {
	return p.MQTTClient.GetChamberTemperature()
}

// NozzleType gets the nozzle type.
func (p *Printer) NozzleType() printerinfo.NozzleType {
	return p.MQTTClient.NozzleType()
}

// NozzleDiameter gets the nozzle diameter.
func (p *Printer) NozzleDiameter() float64 {
	return p.MQTTClient.NozzleDiameter()
}

// GetFileName gets the current/last print file name.
func (p *Printer) GetFileName() string {
	return p.MQTTClient.GetFileName()
}

// GetLightState gets the printer light state.
func (p *Printer) GetLightState() string {
	return p.MQTTClient.GetLightState()
}

// TurnLightOn turns on the printer light.
func (p *Printer) TurnLightOn() error {
	return boolToError(p.MQTTClient.TurnLightOn())
}

// TurnLightOff turns off the printer light.
func (p *Printer) TurnLightOff() error {
	return boolToError(p.MQTTClient.TurnLightOff())
}

// Gcode sends G-code command(s) to the printer.
func (p *Printer) Gcode(gcode any, gcodeCheck bool) (bool, error) {
	return p.MQTTClient.SendGcode(gcode, gcodeCheck)
}

// StartPrint starts printing a file already uploaded to the printer.
func (p *Printer) StartPrint(filename string, plateNumber any, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool) error {
	return boolToError(p.MQTTClient.StartPrint3MF(filename, plateNumber, useAMS, amsMapping, skipObjects, flowCalibration, ""))
}

// StartPrintWithBedType starts printing a file with a specific bed type.
func (p *Printer) StartPrintWithBedType(filename string, plateNumber any, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool, bedType string) error {
	return boolToError(p.MQTTClient.StartPrint3MF(filename, plateNumber, useAMS, amsMapping, skipObjects, flowCalibration, bedType))
}

// SubmitPrintJob is the high-level method to upload a 3MF/Gcode file and start printing.
// This method handles the complete workflow:
// 1. Dynamically connects to FTP
// 2. Uploads the file
// 3. Disconnects from FTP immediately after upload
// 4. Triggers the print job via MQTT
//
// Parameters:
//   - fileData: Reader containing the file data (3MF or Gcode)
//   - filename: Name for the file on the printer
//   - plateNumber: Plate number (int) or plate path (string), defaults to plate 1
//   - useAMS: Whether to use AMS filament system
//   - amsMapping: AMS slot mapping (e.g., [0] for first slot)
//   - flowCalibration: Whether to enable flow calibration
//   - bedType: Bed type (e.g., "textured_plate", "smooth_plate", "" for default)
//
// Returns the uploaded filename on success, or an error on failure.
func (p *Printer) SubmitPrintJob(fileData io.Reader, filename string, plateNumber any, useAMS bool, amsMapping []int, flowCalibration bool, bedType string) (string, error) {
	// Step 1: Upload file via FTP (lazy connection - auto-connects and disconnects)
	uploadedPath, err := p.UploadFile(fileData, filename)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Step 2: Trigger print via MQTT (FTP already disconnected)
	success := p.MQTTClient.StartPrint3MF(uploadedPath, plateNumber, useAMS, amsMapping, nil, flowCalibration, bedType)
	if !success {
		return "", fmt.Errorf("failed to start print job")
	}

	return uploadedPath, nil
}

// SubmitPrintJobFromFile is a convenience method that reads a file from disk and submits it for printing.
// See SubmitPrintJob for parameter details.
func (p *Printer) SubmitPrintJobFromFile(localPath string, plateNumber any, useAMS bool, amsMapping []int, flowCalibration bool, bedType string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Use only the base filename to avoid exposing local directory structure
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(localPath))
	return p.SubmitPrintJob(file, filename, plateNumber, useAMS, amsMapping, flowCalibration, bedType)
}

// StopPrint stops the current print.
func (p *Printer) StopPrint() error {
	return boolToError(p.MQTTClient.StopPrint())
}

// PausePrint pauses the current print.
func (p *Printer) PausePrint() error {
	return boolToError(p.MQTTClient.PausePrint())
}

// ResumePrint resumes a paused print.
func (p *Printer) ResumePrint() error {
	return boolToError(p.MQTTClient.ResumePrint())
}

// SetBedTemperature sets the bed temperature.
func (p *Printer) SetBedTemperature(temperature int) error {
	return boolToError(p.MQTTClient.SetBedTemperature(temperature, false))
}

// SetBedTemperatureOverride sets the bed temperature with override option.
func (p *Printer) SetBedTemperatureOverride(temperature int, override bool) error {
	return boolToError(p.MQTTClient.SetBedTemperature(temperature, override))
}

// HomePrinter homes the printer.
func (p *Printer) HomePrinter() error {
	return boolToError(p.MQTTClient.AutoHome())
}

// SetFilamentPrinter sets the printer filament settings.
func (p *Printer) SetFilamentPrinter(color string, f any, amsID, trayID int) error {
	var settings filament.AMSFilamentSettings

	switch fil := f.(type) {
	case string:
		filamentType, err := filament.FilamentByName(fil)
		if err != nil {
			return fmt.Errorf("invalid filament type: %w", err)
		}
		settings = filamentType.GetSettings()
	case filament.AMSFilamentSettings:
		settings = fil
	default:
		return fmt.Errorf("invalid filament type: %v", f)
	}

	return boolToError(p.MQTTClient.SetPrinterFilament(settings, color, amsID, trayID))
}

// SetNozzleTemperature sets the nozzle temperature.
func (p *Printer) SetNozzleTemperature(temperature int) error {
	return boolToError(p.MQTTClient.SetNozzleTemperature(temperature, false))
}

// SetNozzleTemperatureOverride sets the nozzle temperature with override option.
func (p *Printer) SetNozzleTemperatureOverride(temperature int, override bool) error {
	return boolToError(p.MQTTClient.SetNozzleTemperature(temperature, override))
}

// SetPrintSpeed sets the print speed level (0-3).
func (p *Printer) SetPrintSpeed(speedLevel int) error {
	if speedLevel < 0 || speedLevel > 3 {
		return fmt.Errorf("invalid speed level: %d (must be 0-3)", speedLevel)
	}
	return boolToError(p.MQTTClient.SetPrintSpeedLevel(speedLevel))
}

// CalibratePrinter starts printer calibration.
func (p *Printer) CalibratePrinter(bedLevel, motorNoiseCalibration, vibrationCompensation bool) error {
	return boolToError(p.MQTTClient.Calibration(bedLevel, motorNoiseCalibration, vibrationCompensation))
}

// LoadFilamentSpool loads filament from the spool.
func (p *Printer) LoadFilamentSpool() error {
	return boolToError(p.MQTTClient.LoadFilamentSpool())
}

// UnloadFilamentSpool unloads filament from the spool.
func (p *Printer) UnloadFilamentSpool() error {
	return boolToError(p.MQTTClient.UnloadFilamentSpool())
}

// RetryFilamentAction retries the filament action.
func (p *Printer) RetryFilamentAction() error {
	return boolToError(p.MQTTClient.ResumeFilamentAction())
}

// GetCurrentState gets the current printer status.
func (p *Printer) GetCurrentState() states.PrintStatus {
	return p.MQTTClient.GetCurrentState()
}

// IsBusy returns true if the printer is busy performing a hardware task and cannot accept
// conflicting hardware commands (like homing, calibration, or manual movements).
//
// A printer is considered busy if:
//   - Its GcodeState is RUNNING or PREPARE (actively printing or preparing to print)
//   - OR its PrintStatus indicates an explicit hardware task (not IDLE, not UNKNOWN, not PRINTING)
//
// This method provides a hardware-accurate busy state without requiring arbitrary timeouts.
// Thread-safe: uses existing thread-safe getters GetState() and GetCurrentState().
func (p *Printer) IsBusy() bool {
	gcodeState := p.GetState()
	printStatus := p.GetCurrentState()

	// Check GcodeState first - RUNNING or PREPARE means busy
	if gcodeState == states.GcodeStateRunning || gcodeState == states.GcodeStatePrepare {
		return true
	}

	// Check PrintStatus - busy if not IDLE, not UNKNOWN, and not PRINTING (handled by GcodeState)
	if printStatus != states.PrintStatusIdle &&
		printStatus != states.PrintStatusUnknown &&
		printStatus != states.PrintStatusPrinting {
		return true
	}

	return false
}

// GetActivityDescription returns a human-readable string describing what the printer is currently doing.
//
// Returns:
//   - "IDLE" if the printer is not busy
//   - The GcodeState string (e.g., "RUNNING", "PREPARE") if GcodeState is RUNNING or PREPARE
//   - The PrintStatus string (e.g., "CALIBRATING_MICRO_LIDAR", "HOMING_TOOLHEAD") if busy due to hardware task
//
// This method is designed for frontend display to inform users about current printer activity.
// Thread-safe: uses existing thread-safe getters GetState() and GetCurrentState().
func (p *Printer) GetActivityDescription() string {
	gcodeState := p.GetState()
	printStatus := p.GetCurrentState()

	// If not busy, return IDLE
	if !p.IsBusy() {
		return "IDLE"
	}

	// If GcodeState is RUNNING or PREPARE, return that state
	if gcodeState == states.GcodeStateRunning || gcodeState == states.GcodeStatePrepare {
		return gcodeState.String()
	}

	// Otherwise, return the PrintStatus string (hardware task description)
	return printStatus.String()
}

// GetSkippedObjects gets the list of skipped objects.
func (p *Printer) GetSkippedObjects() []int {
	return p.MQTTClient.GetSkippedObjects()
}

// SkipObjectsskips objects during printing.
func (p *Printer) SkipObjects(objList []int) error {
	return boolToError(p.MQTTClient.SkipObjects(objList))
}

// SetPartFanSpeed sets the part fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetPartFanSpeed(speed any) (bool, error) {
	return p.MQTTClient.SetPartFanSpeed(speed)
}

// SetPartFanSpeedInt sets the part fan speed (0-255).
func (p *Printer) SetPartFanSpeedInt(speed int) error {
	return boolToError(p.MQTTClient.SetPartFanSpeedInt(speed))
}

// SetAuxFanSpeed sets the auxiliary fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetAuxFanSpeed(speed any) (bool, error) {
	return p.MQTTClient.SetAuxFanSpeed(speed)
}

// SetAuxFanSpeedInt sets the aux fan speed (0-255).
func (p *Printer) SetAuxFanSpeedInt(speed int) error {
	return boolToError(p.MQTTClient.SetAuxFanSpeedInt(speed))
}

// SetChamberFanSpeed sets the chamber fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetChamberFanSpeed(speed any) (bool, error) {
	return p.MQTTClient.SetChamberFanSpeed(speed)
}

// SetChamberFanSpeedInt sets the chamber fan speed (0-255).
func (p *Printer) SetChamberFanSpeedInt(speed int) error {
	return boolToError(p.MQTTClient.SetChamberFanSpeedInt(speed))
}

// VTTray gets the external spool filament tray.
func (p *Printer) VTTray() filament.FilamentTray {
	return p.MQTTClient.VTTray()
}

// AMSHub gets the AMS hub with all connected AMS units.
func (p *Printer) AMSHub() *ams.AMSHub {
	p.MQTTClient.ProcessAMS()
	return p.MQTTClient.AMSHub()
}

// SubtaskName gets the current subtask name.
func (p *Printer) SubtaskName() string {
	return p.MQTTClient.SubtaskName()
}

// GcodeFile gets the current gcode file name.
func (p *Printer) GcodeFile() string {
	return p.MQTTClient.GcodeFile()
}

// PrintErrorCode gets the print error code.
func (p *Printer) PrintErrorCode() int {
	return p.MQTTClient.PrintErrorCode()
}

// PrintType gets the print type (cloud/local).
func (p *Printer) PrintType() string {
	return p.MQTTClient.PrintType()
}

// WifiSignal gets the WiFi signal strength in dBm.
func (p *Printer) WifiSignal() string {
	return p.MQTTClient.WifiSignal()
}

// Reboot reboots the printer.
func (p *Printer) Reboot() error {
	return boolToError(p.MQTTClient.Reboot())
}

// SetNozzleInfo sets the nozzle information.
func (p *Printer) SetNozzleInfo(nozzleType printerinfo.NozzleType, nozzleDiameter float64) error {
	return boolToError(p.MQTTClient.SetNozzleInfo(nozzleType, nozzleDiameter))
}

// GetPartFanSpeed gets the part fan speed.
func (p *Printer) GetPartFanSpeed() int {
	return p.MQTTClient.GetPartFanSpeed()
}

// GetAuxFanSpeed gets the auxiliary fan speed.
func (p *Printer) GetAuxFanSpeed() int {
	return p.MQTTClient.GetAuxFanSpeed()
}

// GetChamberFanSpeed gets the chamber fan speed.
func (p *Printer) GetChamberFanSpeed() int {
	return p.MQTTClient.GetChamberFanSpeed()
}

// GetFanGear gets the consolidated fan value.
func (p *Printer) GetFanGear() int {
	return p.MQTTClient.GetFanGear()
}

// ============================================
// LAZY-LOADED FTP OPERATIONS
// These methods auto-connect/disconnect FTP
// ============================================

// withFTP executes a function with automatic FTP connection management.
// The FTP connection is established on-demand and closed after the operation.
func (p *Printer) withFTP(fn func(*ftp.PrinterFTPClient) error) error {
	// Auto-connect
	if err := p.FTPClient.Reconnect(); err != nil {
		return fmt.Errorf("failed to connect to FTP: %w", err)
	}
	defer func() { _ = p.FTPClient.Close() }() // Auto-disconnect after operation

	return fn(p.FTPClient)
}

// UploadFile uploads a file to the printer via FTP (auto-connects/disconnects).
func (p *Printer) UploadFile(file io.Reader, filename string) (string, error) {
	var result string
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.UploadFile(file, filename)
		return err
	})
	return result, err
}

// UploadFileFromPath uploads a file from disk to the printer (auto-connects/disconnects).
func (p *Printer) UploadFileFromPath(localPath, remoteFilename string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()
	return p.UploadFile(file, remoteFilename)
}

// DownloadFile downloads a file from the printer via FTP (auto-connects/disconnects).
func (p *Printer) DownloadFile(filePath string) ([]byte, error) {
	var result []byte
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.DownloadFile(filePath)
		return err
	})
	return result, err
}

// DownloadFileToPath downloads a file from the printer to disk (auto-connects/disconnects).
func (p *Printer) DownloadFileToPath(remotePath, localPath string) error {
	data, err := p.DownloadFile(remotePath)
	if err != nil {
		return err
	}
	return os.WriteFile(localPath, data, 0600)
}

// DeleteFile deletes a file from the printer via FTP (auto-connects/disconnects).
func (p *Printer) DeleteFile(filePath string) error {
	return p.withFTP(func(client *ftp.PrinterFTPClient) error {
		return client.DeleteFile(filePath)
	})
}

// ListDirectory lists files in a directory on the printer (auto-connects/disconnects).
func (p *Printer) ListDirectory(path string) ([]string, error) {
	var result []string
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.ListDirectory(path)
		return err
	})
	return result, err
}

// ListImagesDir lists files in the image directory (auto-connects/disconnects).
func (p *Printer) ListImagesDir() ([]string, error) {
	var result []string
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.ListImagesDir()
		return err
	})
	return result, err
}

// ListTimelapseDir lists files in the timelapse directory (auto-connects/disconnects).
func (p *Printer) ListTimelapseDir() ([]string, error) {
	var result []string
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.ListTimelapseDir()
		return err
	})
	return result, err
}

// GetLastImagePrint gets the last image from the image directory (auto-connects/disconnects).
func (p *Printer) GetLastImagePrint() ([]byte, error) {
	var result []byte
	err := p.withFTP(func(client *ftp.PrinterFTPClient) error {
		var err error
		result, err = client.GetLastImagePrint()
		return err
	})
	return result, err
}

// ============================================
// LAZY-LOADED CAMERA OPERATIONS
// Camera is started/stopped on-demand
// ============================================

// DefaultCameraFrameTimeout is the default timeout for waiting for a camera frame.
const DefaultCameraFrameTimeout = 10 * time.Second

// StartCamera starts the camera stream (lazy loading).
func (p *Printer) StartCamera() bool {
	return p.CameraClient.Start()
}

// StopCamera stops the camera stream.
func (p *Printer) StopCamera() {
	p.CameraClient.Stop()
}

// CameraIsAlive checks if the camera stream is running.
func (p *Printer) CameraIsAlive() bool {
	return p.CameraClient.IsAlive()
}

// GetCameraFrame gets the latest camera frame as base64 (auto-starts camera if needed).
// Waits up to DefaultCameraFrameTimeout for a frame to become available.
func (p *Printer) GetCameraFrame() (string, error) {
	// Auto-start camera if not running
	if !p.CameraIsAlive() {
		p.StartCamera()
	}

	// Wait for frame with timeout
	frameBytes, err := p.CameraClient.WaitForFrame(DefaultCameraFrameTimeout)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(frameBytes), nil
}

// GetCameraFrameBytes gets the latest camera frame as bytes (auto-starts camera if needed).
// Waits up to DefaultCameraFrameTimeout for a frame to become available.
func (p *Printer) GetCameraFrameBytes() ([]byte, error) {
	// Auto-start camera if not running
	if !p.CameraIsAlive() {
		p.StartCamera()
	}

	// Wait for frame with timeout
	return p.CameraClient.WaitForFrame(DefaultCameraFrameTimeout)
}

// GetCameraFrameWithTimeout gets the latest camera frame as bytes with a custom timeout.
// Auto-starts camera if needed.
func (p *Printer) GetCameraFrameWithTimeout(timeout time.Duration) ([]byte, error) {
	// Auto-start camera if not running
	if !p.CameraIsAlive() {
		p.StartCamera()
	}

	// Wait for frame with custom timeout
	return p.CameraClient.WaitForFrame(timeout)
}

// GetCameraImage gets the latest camera frame as an image.Image (auto-starts camera if needed).
// Waits up to DefaultCameraFrameTimeout for a frame to become available.
func (p *Printer) GetCameraImage() (image.Image, error) {
	frameBytes, err := p.GetCameraFrameBytes()
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(frameBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// SaveCameraFrame saves the latest camera frame to a file (auto-starts camera if needed).
// Waits up to DefaultCameraFrameTimeout for a frame to become available.
func (p *Printer) SaveCameraFrame(filePath string) error {
	frameBytes, err := p.GetCameraFrameBytes()
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, frameBytes, 0600)
}

// CaptureFrame captures a single frame and returns it (convenience method).
// This starts the camera, waits for one frame (up to 10 seconds), and stops the camera.
func (p *Printer) CaptureFrame() ([]byte, error) {
	// Start camera
	p.StartCamera()
	defer p.StopCamera()

	// Wait for first frame with timeout
	return p.CameraClient.WaitForFrame(DefaultCameraFrameTimeout)
}

// CaptureFrameWithTimeout captures a single frame with a custom timeout.
// This starts the camera, waits for one frame, and stops the camera.
func (p *Printer) CaptureFrameWithTimeout(timeout time.Duration) ([]byte, error) {
	// Start camera
	p.StartCamera()
	defer p.StopCamera()

	// Wait for first frame with custom timeout
	return p.CameraClient.WaitForFrame(timeout)
}

// GetIPAddress returns the printer's IP address.
func (p *Printer) GetIPAddress() string {
	return p.IPAddress
}

// GetSerial returns the printer's serial number.
func (p *Printer) GetSerial() string {
	return p.Serial
}

// GetMQTTClient returns the MQTT client as an interface for testability.
func (p *Printer) GetMQTTClient() mqtt.MQTTClient {
	return p.MQTTClient
}

// GetFTPClient returns the FTP client as an interface for testability.
func (p *Printer) GetFTPClient() ftp.FTPClient {
	return p.FTPClient
}

// GetCameraClient returns the camera client as an interface for testability.
func (p *Printer) GetCameraClient() camera.CameraClient {
	return p.CameraClient
}
