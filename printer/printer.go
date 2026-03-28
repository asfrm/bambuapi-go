// Package printer provides the main client for connecting to and controlling Bambu Lab 3D printers.
package printer

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"

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
func (p *Printer) Connect() error {
	err := p.MQTTClient.Start()
	if err != nil {
		return fmt.Errorf("failed to start MQTT client: %w", err)
	}
	p.CameraClient.Start()
	return nil
}

// Disconnect disconnects from the printer.
func (p *Printer) Disconnect() {
	p.MQTTClient.Stop()
	p.CameraClient.Stop()
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

// UploadFile uploads a file to the printer via FTP.
func (p *Printer) UploadFile(file io.Reader, filename string) (string, error) {
	if filename == "" {
		filename = "ftp_upload.gcode"
	}
	return p.FTPClient.UploadFile(file, filename)
}

// StartPrint starts printing a file.
func (p *Printer) StartPrint(filename string, plateNumber interface{}, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool) bool {
	return p.MQTTClient.StartPrint3MF(filename, plateNumber, useAMS, amsMapping, skipObjects, flowCalibration)
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

// DeleteFile deletes a file from the printer.
func (p *Printer) DeleteFile(filePath string) error {
	return p.FTPClient.DeleteFile(filePath)
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

// GetCameraFrame gets the camera frame as base64 encoded string.
func (p *Printer) GetCameraFrame() (string, error) {
	return p.CameraClient.GetFrame()
}

// GetCameraImage gets the camera frame as an image.
func (p *Printer) GetCameraImage() (image.Image, error) {
	frameBase64, err := p.CameraClient.GetFrame()
	if err != nil {
		return nil, err
	}

	frameBytes, err := base64.StdEncoding.DecodeString(frameBase64)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(frameBytes))
	if err != nil {
		return nil, err
	}

	return img, nil
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

// ListImagesDir lists files in the image directory.
func (p *Printer) ListImagesDir() ([]string, error) {
	return p.FTPClient.ListImagesDir()
}

// ListCacheDir lists files in the cache directory.
func (p *Printer) ListCacheDir() ([]string, error) {
	return p.FTPClient.ListCacheDir()
}

// ListTimelapseDir lists files in the timelapse directory.
func (p *Printer) ListTimelapseDir() ([]string, error) {
	return p.FTPClient.ListTimelapseDir()
}

// ListLoggerDir lists files in the logger directory.
func (p *Printer) ListLoggerDir() ([]string, error) {
	return p.FTPClient.ListLoggerDir()
}

// DownloadFile downloads a file from the printer.
func (p *Printer) DownloadFile(filePath string) ([]byte, error) {
	return p.FTPClient.DownloadFile(filePath)
}

// GetLastImagePrint gets the last image from the image directory.
func (p *Printer) GetLastImagePrint() ([]byte, error) {
	return p.FTPClient.GetLastImagePrint()
}

// GetFanGear gets the consolidated fan value.
func (p *Printer) GetFanGear() int {
	return p.MQTTClient.GetFanGear()
}
