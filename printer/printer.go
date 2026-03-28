// Package printer provides the main client for connecting to and controlling Bambu Lab 3D printers.
package printer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"os"
	"time"

	"github.com/asfrm/bambuapi-go/ams"
	"github.com/asfrm/bambuapi-go/camera"
	"github.com/asfrm/bambuapi-go/filament"
	"github.com/asfrm/bambuapi-go/ftp"
	"github.com/asfrm/bambuapi-go/mqtt"
	"github.com/asfrm/bambuapi-go/printerinfo"
	"github.com/asfrm/bambuapi-go/states"
)

// Printer is the main client for connecting to and controlling a Bambu Lab 3D printer.
type Printer struct {
	IPAddress  string
	AccessCode string
	Serial     string

	MQTTClient   *mqtt.PrinterMQTTClient
	CameraClient *camera.PrinterCamera
	FTPClient    *ftp.PrinterFTPClient
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

	for {
		select {
		case <-timeout:
			p.MQTTClient.Stop()
			return fmt.Errorf("timeout waiting for MQTT connection")
		case <-ticker.C:
			if p.MQTTClient.IsConnected() {
				ticker.Stop()
				break
			}
		}
		break
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
			if printData, ok := dump["print"].(map[string]interface{}); ok {
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

// SetMQTTAggressiveMode sets whether to send aggressive pushall/info requests on connect.
// Set to false for CLI usage to avoid blocking.
func (p *Printer) SetMQTTAggressiveMode(enabled bool) {
	p.MQTTClient.SetPushallAggressive(enabled)
}

// RequestFullState requests a full state update from the printer.
func (p *Printer) RequestFullState() bool {
	return p.MQTTClient.RequestFullState()
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
func (p *Printer) CameraStart() bool {
	return p.CameraClient.Start()
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
func (p *Printer) MQTTDump() map[string]interface{} {
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
func (p *Printer) TurnLightOn() bool {
	return p.MQTTClient.TurnLightOn()
}

// TurnLightOff turns off the printer light.
func (p *Printer) TurnLightOff() bool {
	return p.MQTTClient.TurnLightOff()
}

// Gcode sends G-code command(s) to the printer.
func (p *Printer) Gcode(gcode interface{}, gcodeCheck bool) (bool, error) {
	return p.MQTTClient.SendGcode(gcode, gcodeCheck)
}

// StartPrint starts printing a file already uploaded to the printer.
func (p *Printer) StartPrint(filename string, plateNumber interface{}, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool) bool {
	return p.MQTTClient.StartPrint3MF(filename, plateNumber, useAMS, amsMapping, skipObjects, flowCalibration, "")
}

// StartPrintWithBedType starts printing a file with a specific bed type.
func (p *Printer) StartPrintWithBedType(filename string, plateNumber interface{}, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool, bedType string) bool {
	return p.MQTTClient.StartPrint3MF(filename, plateNumber, useAMS, amsMapping, skipObjects, flowCalibration, bedType)
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
func (p *Printer) SubmitPrintJob(fileData io.Reader, filename string, plateNumber interface{}, useAMS bool, amsMapping []int, flowCalibration bool, bedType string) (string, error) {
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
func (p *Printer) SubmitPrintJobFromFile(localPath string, plateNumber interface{}, useAMS bool, amsMapping []int, flowCalibration bool, bedType string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), localPath)
	return p.SubmitPrintJob(file, filename, plateNumber, useAMS, amsMapping, flowCalibration, bedType)
}

// StopPrint stops the current print.
func (p *Printer) StopPrint() bool {
	return p.MQTTClient.StopPrint()
}

// PausePrint pauses the current print.
func (p *Printer) PausePrint() bool {
	return p.MQTTClient.PausePrint()
}

// ResumePrint resumes a paused print.
func (p *Printer) ResumePrint() bool {
	return p.MQTTClient.ResumePrint()
}

// SetBedTemperature sets the bed temperature.
func (p *Printer) SetBedTemperature(temperature int) bool {
	return p.MQTTClient.SetBedTemperature(temperature, false)
}

// SetBedTemperatureOverride sets the bed temperature with override option.
func (p *Printer) SetBedTemperatureOverride(temperature int, override bool) bool {
	return p.MQTTClient.SetBedTemperature(temperature, override)
}

// HomePrinter homes the printer.
func (p *Printer) HomePrinter() bool {
	return p.MQTTClient.AutoHome()
}

// MoveZAxis moves the Z-axis to a specific height.
func (p *Printer) MoveZAxis(height int) bool {
	return p.MQTTClient.SetBedHeight(height)
}

// SetFilamentPrinter sets the printer filament settings.
func (p *Printer) SetFilamentPrinter(color string, f interface{}, amsID, trayID int) bool {
	var settings filament.AMSFilamentSettings

	switch fil := f.(type) {
	case string:
		filamentType, err := filament.FilamentByName(fil)
		if err != nil {
			return false
		}
		settings = filamentType.GetSettings()
	case filament.AMSFilamentSettings:
		settings = fil
	default:
		return false
	}

	return p.MQTTClient.SetPrinterFilament(settings, color, amsID, trayID)
}

// SetNozzleTemperature sets the nozzle temperature.
func (p *Printer) SetNozzleTemperature(temperature int) bool {
	return p.MQTTClient.SetNozzleTemperature(temperature, false)
}

// SetNozzleTemperatureOverride sets the nozzle temperature with override option.
func (p *Printer) SetNozzleTemperatureOverride(temperature int, override bool) bool {
	return p.MQTTClient.SetNozzleTemperature(temperature, override)
}

// SetPrintSpeed sets the print speed level (0-3).
func (p *Printer) SetPrintSpeed(speedLevel int) bool {
	if speedLevel < 0 || speedLevel > 3 {
		return false
	}
	return p.MQTTClient.SetPrintSpeedLevel(speedLevel)
}

// CalibratePrinter starts printer calibration.
func (p *Printer) CalibratePrinter(bedLevel, motorNoiseCalibration, vibrationCompensation bool) bool {
	return p.MQTTClient.Calibration(bedLevel, motorNoiseCalibration, vibrationCompensation)
}

// LoadFilamentSpool loads filament from the spool.
func (p *Printer) LoadFilamentSpool() bool {
	return p.MQTTClient.LoadFilamentSpool()
}

// UnloadFilamentSpool unloads filament from the spool.
func (p *Printer) UnloadFilamentSpool() bool {
	return p.MQTTClient.UnloadFilamentSpool()
}

// RetryFilamentAction retries the filament action.
func (p *Printer) RetryFilamentAction() bool {
	return p.MQTTClient.ResumeFilamentAction()
}

// GetCurrentState gets the current printer status.
func (p *Printer) GetCurrentState() states.PrintStatus {
	return p.MQTTClient.GetCurrentState()
}

// GetSkippedObjects gets the list of skipped objects.
func (p *Printer) GetSkippedObjects() []int {
	return p.MQTTClient.GetSkippedObjects()
}

// SkipObjects skips objects during printing.
func (p *Printer) SkipObjects(objList []int) bool {
	return p.MQTTClient.SkipObjects(objList)
}

// SetPartFanSpeed sets the part fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetPartFanSpeed(speed interface{}) (bool, error) {
	return p.MQTTClient.SetPartFanSpeed(speed)
}

// SetPartFanSpeedInt sets the part fan speed (0-255).
func (p *Printer) SetPartFanSpeedInt(speed int) bool {
	return p.MQTTClient.SetPartFanSpeedInt(speed)
}

// SetAuxFanSpeed sets the auxiliary fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetAuxFanSpeed(speed interface{}) (bool, error) {
	return p.MQTTClient.SetAuxFanSpeed(speed)
}

// SetAuxFanSpeedInt sets the aux fan speed (0-255).
func (p *Printer) SetAuxFanSpeedInt(speed int) bool {
	return p.MQTTClient.SetAuxFanSpeedInt(speed)
}

// SetChamberFanSpeed sets the chamber fan speed (0-255 or 0.0-1.0).
func (p *Printer) SetChamberFanSpeed(speed interface{}) (bool, error) {
	return p.MQTTClient.SetChamberFanSpeed(speed)
}

// SetChamberFanSpeedInt sets the chamber fan speed (0-255).
func (p *Printer) SetChamberFanSpeedInt(speed int) bool {
	return p.MQTTClient.SetChamberFanSpeedInt(speed)
}

// SetAutoStepRecovery sets auto step recovery.
func (p *Printer) SetAutoStepRecovery(autoStepRecovery bool) bool {
	return p.MQTTClient.SetAutoStepRecovery(autoStepRecovery)
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
func (p *Printer) Reboot() bool {
	return p.MQTTClient.Reboot()
}

// SetOnboardPrinterTimelapse enables/disables onboard timelapse.
func (p *Printer) SetOnboardPrinterTimelapse(enable bool) bool {
	return p.MQTTClient.SetOnboardPrinterTimelapse(enable)
}

// SetNozzleInfo sets the nozzle information.
func (p *Printer) SetNozzleInfo(nozzleType printerinfo.NozzleType, nozzleDiameter float64) bool {
	return p.MQTTClient.SetNozzleInfo(nozzleType, nozzleDiameter)
}

// NewPrinterFirmware checks if new firmware is available.
func (p *Printer) NewPrinterFirmware() string {
	return p.MQTTClient.NewPrinterFirmware()
}

// UpgradeFirmware upgrades to the latest firmware.
func (p *Printer) UpgradeFirmware(override bool) bool {
	return p.MQTTClient.UpgradeFirmware(override)
}

// DowngradeFirmware downgrades to a specific firmware version.
func (p *Printer) DowngradeFirmware(firmwareVersion string) bool {
	return p.MQTTClient.DowngradeFirmware(firmwareVersion)
}

// GetAccessCode gets the access code.
func (p *Printer) GetAccessCode() string {
	return p.MQTTClient.GetAccessCode()
}

// RequestAccessCode requests the access code from the printer.
func (p *Printer) RequestAccessCode() bool {
	return p.MQTTClient.RequestAccessCode()
}

// GetFirmwareHistory gets the firmware history.
func (p *Printer) GetFirmwareHistory() []map[string]interface{} {
	return p.MQTTClient.GetFirmwareHistory()
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
	defer p.FTPClient.Close() // Auto-disconnect after operation

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
	defer file.Close()
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
	return os.WriteFile(localPath, data, 0644)
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
	return os.WriteFile(filePath, frameBytes, 0644)
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
