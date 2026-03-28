// Package mqtt provides MQTT communication functionality for Bambu Lab printers.
package mqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/asfrm/bambuapi-go/ams"
	"github.com/asfrm/bambuapi-go/filament"
	"github.com/asfrm/bambuapi-go/printerinfo"
	"github.com/asfrm/bambuapi-go/states"
)

var (
	debugLog = log.New(os.Stdout, "[DEBUG] ", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
	errorLog = log.New(os.Stderr, "[ERROR] ", log.Ldate|log.Ltime|log.Lmicroseconds|log.Lshortfile)
	infoLog  = log.New(os.Stdout, "[INFO] ", log.Ldate|log.Ltime|log.Lmicroseconds)
	warnLog  = log.New(os.Stdout, "[WARN] ", log.Ldate|log.Ltime|log.Lmicroseconds)
)

// PrinterMQTTClient handles MQTT communication with the printer.
type PrinterMQTTClient struct {
	hostname      string
	access        string
	username      string
	printerSerial string
	port          int
	timeout       int

	client       mqtt.Client
	commandTopic string

	mu                sync.RWMutex
	data              map[string]interface{}
	lastUpdate        int64
	pushallTimeout    int
	pushallAggressive bool

	amsHub      *ams.AMSHub
	strict      bool
	printerInfo printerinfo.PrinterFirmwareInfo

	connected bool
	ready     bool
}

// NewPrinterMQTTClient creates a new MQTT client.
func NewPrinterMQTTClient(hostname, access, printerSerial string, username string, port, timeout, pushallTimeout int, pushallOnConnect, strict bool) *PrinterMQTTClient {
	if username == "" {
		username = "bblp"
	}
	if port == 0 {
		port = 8883
	}
	if timeout == 0 {
		timeout = 60
	}
	if pushallTimeout == 0 {
		pushallTimeout = 60
	}

	c := &PrinterMQTTClient{
		hostname:          hostname,
		access:            access,
		username:          username,
		printerSerial:     printerSerial,
		port:              port,
		timeout:           timeout,
		commandTopic:      fmt.Sprintf("device/%s/request", printerSerial),
		data:              make(map[string]interface{}),
		lastUpdate:        0,
		pushallTimeout:    pushallTimeout,
		pushallAggressive: pushallOnConnect,
		amsHub:            ams.NewAMSHub(),
		strict:            strict,
		printerInfo: printerinfo.PrinterFirmwareInfo{
			PrinterType:     printerinfo.PrinterTypeP1S,
			FirmwareVersion: "01.04.00.00",
		},
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tls://%s:%d", hostname, port))
	opts.SetUsername(username)
	opts.SetPassword(access)
	opts.SetClientID(printerSerial)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetConnectionLostHandler(c.onConnectionLost)
	opts.SetOnConnectHandler(c.onConnect)
	opts.SetDefaultPublishHandler(c.onMessage)

	// TLS configuration
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	opts.SetTLSConfig(tlsConfig)

	c.client = mqtt.NewClient(opts)

	return c
}

// IsConnected checks if the MQTT client is connected.
func (c *PrinterMQTTClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client.IsConnected()
}

// Ready checks if the client has received data.
func (c *PrinterMQTTClient) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready && len(c.data) > 0
}

// SetPushallAggressive sets whether to send pushall/info requests on connect.
func (c *PrinterMQTTClient) SetPushallAggressive(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pushallAggressive = enabled
}

// Connect connects to the MQTT server.
func (c *PrinterMQTTClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		errorLog.Printf("Connection failed: %v", token.Error())
		return token.Error()
	}

	infoLog.Println("Connected successfully")
	return nil
}

// Start starts the MQTT client loop.
func (c *PrinterMQTTClient) Start() error {
	if !c.client.IsConnected() {
		if err := c.Connect(); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops the MQTT client.
func (c *PrinterMQTTClient) Stop() {
	// Disconnect in a goroutine to avoid blocking
	done := make(chan struct{})
	go func() {
		c.client.Disconnect(100)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		// Timeout, but continue anyway
	}

	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
	infoLog.Println("MQTT client stopped")
}

// onConnect is called when connected to MQTT.
func (c *PrinterMQTTClient) onConnect(client mqtt.Client) {
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	infoLog.Println("Connected to MQTT server")

	// Subscribe to report topic
	reportTopic := fmt.Sprintf("device/%s/report", c.printerSerial)
	token := client.Subscribe(reportTopic, 1, nil)
	token.Wait()

	if c.pushallAggressive {
		c.pushall()
		c.infoGetVersion()
		c.requestFirmwareHistory()
	}

	infoLog.Println("Connection handshake completed")
}

// onConnectionLost is called when connection is lost.
func (c *PrinterMQTTClient) onConnectionLost(client mqtt.Client, err error) {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
	errorLog.Printf("Connection lost: %v", err)
}

// onMessage handles incoming MQTT messages.
func (c *PrinterMQTTClient) onMessage(client mqtt.Client, msg mqtt.Message) {
	var doc map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &doc); err != nil {
		errorLog.Printf("Failed to parse message: %v", err)
		return
	}

	c.manualUpdate(doc)
}

// manualUpdate updates internal data from received message.
func (c *PrinterMQTTClient) manualUpdate(doc map[string]interface{}) {
	c.mu.Lock()

	for k, v := range doc {
		if existing, ok := c.data[k]; ok {
			if existingMap, ok := existing.(map[string]interface{}); ok {
				if newMap, ok := v.(map[string]interface{}); ok {
					for nk, nv := range newMap {
						existingMap[nk] = nv
					}
					continue
				}
			}
		}
		c.data[k] = v
	}

	c.ready = true
	c.mu.Unlock()

	// Update firmware version AFTER releasing lock to avoid deadlock
	// (getFirmwareVersion acquires read lock)
	if firmwareVersion := c.getFirmwareVersion(); firmwareVersion != "" {
		c.mu.Lock()
		c.printerInfo.FirmwareVersion = firmwareVersion
		c.mu.Unlock()
	}
}

// getMapKeys returns the top-level keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// getPrintValue gets a value from the "print" section.
func (c *PrinterMQTTClient) getPrintValue(key string, defaultValue interface{}) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if printData, ok := c.data["print"].(map[string]interface{}); ok {
		if val, ok := printData[key]; ok {
			return val
		}
	}
	return defaultValue
}

// getInfoValue gets a value from the "info" section.
func (c *PrinterMQTTClient) getInfoValue(key string, defaultValue interface{}) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if infoData, ok := c.data["info"].(map[string]interface{}); ok {
		if val, ok := infoData[key]; ok {
			return val
		}
	}
	return defaultValue
}

// getFloat64 safely extracts a float64 value from the "print" section.
// It handles int, float64, and string types, converting them to float64.
// Returns defaultValue if the key doesn't exist or conversion fails.
func (c *PrinterMQTTClient) getFloat64(key string, defaultValue float64) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if printData, ok := c.data["print"].(map[string]interface{}); ok {
		if val, ok := printData[key]; ok {
			switch v := val.(type) {
			case float64:
				return v
			case int:
				return float64(v)
			case int64:
				return float64(v)
			case string:
				if parsed, err := strconv.ParseFloat(v, 64); err == nil {
					return parsed
				}
			case json.Number:
				if parsed, err := v.Float64(); err == nil {
					return parsed
				}
			}
		}
	}
	return defaultValue
}

// getInt safely extracts an int value from the "print" section.
// It handles int, float64, and string types, converting them to int.
// Returns defaultValue if the key doesn't exist or conversion fails.
func (c *PrinterMQTTClient) getInt(key string, defaultValue int) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if printData, ok := c.data["print"].(map[string]interface{}); ok {
		if val, ok := printData[key]; ok {
			switch v := val.(type) {
			case float64:
				return int(v)
			case int:
				return v
			case int64:
				return int(v)
			case string:
				if parsed, err := strconv.Atoi(v); err == nil {
					return parsed
				}
			case json.Number:
				if parsed, err := v.Int64(); err == nil {
					return int(parsed)
				}
			}
		}
	}
	return defaultValue
}

// getString safely extracts a string value from the "print" section.
// It handles string types and converts other types to string if possible.
// Returns defaultValue if the key doesn't exist or conversion fails.
func (c *PrinterMQTTClient) getString(key string, defaultValue string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if printData, ok := c.data["print"].(map[string]interface{}); ok {
		if val, ok := printData[key]; ok {
			switch v := val.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%v", v)
			case int:
				return fmt.Sprintf("%d", v)
			case bool:
				return fmt.Sprintf("%v", v)
			}
		}
	}
	return defaultValue
}

// getPrintMap safely gets the "print" map for nested access.
// Returns nil if not available.
func (c *PrinterMQTTClient) getPrintMap() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if printData, ok := c.data["print"].(map[string]interface{}); ok {
		return printData
	}
	return nil
}

// publishCommand publishes a command to the MQTT server.
func (c *PrinterMQTTClient) publishCommand(payload map[string]interface{}) bool {
	if !c.client.IsConnected() {
		errorLog.Println("Not connected to MQTT server")
		return false
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		errorLog.Printf("Failed to marshal payload: %v", err)
		return false
	}

	token := c.client.Publish(c.commandTopic, 1, false, jsonData)
	// Use WaitTimeout instead of Wait to avoid indefinite blocking
	if token.WaitTimeout(2 * time.Second) {
		return token.Error() == nil
	}
	// Timeout occurred, but command may still have been sent
	return true
}

// pushall forces a full state update from the printer.
func (c *PrinterMQTTClient) pushall() bool {
	return c.publishCommand(map[string]interface{}{
		"pushing": map[string]interface{}{
			"command": "pushall",
		},
	})
}

// RequestFullState requests a full state update from the printer.
func (c *PrinterMQTTClient) RequestFullState() bool {
	return c.pushall()
}

// infoGetVersion requests hardware and firmware info.
func (c *PrinterMQTTClient) infoGetVersion() bool {
	return c.publishCommand(map[string]interface{}{
		"info": map[string]interface{}{
			"command": "get_version",
		},
	})
}

// requestFirmwareHistory requests firmware history.
func (c *PrinterMQTTClient) requestFirmwareHistory() bool {
	return c.publishCommand(map[string]interface{}{
		"upgrade": map[string]interface{}{
			"command": "get_history",
		},
	})
}

// getFirmwareVersion gets the current firmware version.
func (c *PrinterMQTTClient) getFirmwareVersion() string {
	modules, ok := c.getInfoValue("module", []interface{}{}).([]interface{})
	if !ok {
		return ""
	}

	for _, m := range modules {
		if module, ok := m.(map[string]interface{}); ok {
			if name, ok := module["name"].(string); ok && name == "ota" {
				if swVer, ok := module["sw_ver"].(string); ok {
					return swVer
				}
			}
		}
	}
	return ""
}

// GetLastPrintPercentage gets the print completion percentage.
func (c *PrinterMQTTClient) GetLastPrintPercentage() int {
	return c.getInt("mc_percent", 0)
}

// GetRemainingTime gets the remaining print time in seconds.
func (c *PrinterMQTTClient) GetRemainingTime() int {
	return c.getInt("mc_remaining_time", 0)
}

// GetPrinterState gets the printer G-code state.
func (c *PrinterMQTTClient) GetPrinterState() states.GcodeState {
	state := c.getString("gcode_state", "")
	return states.ParseGcodeState(state)
}

// GetCurrentState gets the current printer status.
func (c *PrinterMQTTClient) GetCurrentState() states.PrintStatus {
	status := c.getInt("stg_cur", -1)
	return states.ParsePrintStatus(status)
}

// GetPrintSpeed gets the print speed.
func (c *PrinterMQTTClient) GetPrintSpeed() int {
	return c.getInt("spd_mag", 100)
}

// GetFileName gets the current/last print file name.
func (c *PrinterMQTTClient) GetFileName() string {
	return c.getString("gcode_file", "")
}

// GetLightState gets the printer light state.
func (c *PrinterMQTTClient) GetLightState() string {
	lightsReport := c.getPrintValue("lights_report", []interface{}{})
	if lightsReport == nil {
		return "unknown"
	}

	lights, ok := lightsReport.([]interface{})
	if !ok || len(lights) == 0 {
		return "unknown"
	}

	if light, ok := lights[0].(map[string]interface{}); ok {
		if mode, ok := light["mode"].(string); ok {
			return mode
		}
	}
	return "unknown"
}

// TurnLightOn turns on the printer light.
func (c *PrinterMQTTClient) TurnLightOn() bool {
	return c.publishCommand(map[string]interface{}{
		"system": map[string]interface{}{
			"led_mode": "on",
		},
	})
}

// TurnLightOff turns off the printer light.
func (c *PrinterMQTTClient) TurnLightOff() bool {
	return c.publishCommand(map[string]interface{}{
		"system": map[string]interface{}{
			"led_mode": "off",
		},
	})
}

// GetBedTemperature gets the bed temperature.
func (c *PrinterMQTTClient) GetBedTemperature() float64 {
	return c.getFloat64("bed_temper", 0.0)
}

// GetNozzleTemperature gets the nozzle temperature.
func (c *PrinterMQTTClient) GetNozzleTemperature() float64 {
	return c.getFloat64("nozzle_temper", 0.0)
}

// GetChamberTemperature gets the chamber temperature.
func (c *PrinterMQTTClient) GetChamberTemperature() float64 {
	temp := c.getFloat64("chamber_temper", -999.0)
	if temp != -999.0 {
		return temp
	}

	// Fallback to device.ctc.info.temp
	printMap := c.getPrintMap()
	if printMap == nil {
		return 0.0
	}

	if device, ok := printMap["device"].(map[string]interface{}); ok {
		if ctc, ok := device["ctc"].(map[string]interface{}); ok {
			if info, ok := ctc["info"].(map[string]interface{}); ok {
				if t, ok := info["temp"].(float64); ok {
					return t
				}
			}
		}
	}
	return 0.0
}

// CurrentLayerNum gets the current layer number.
func (c *PrinterMQTTClient) CurrentLayerNum() int {
	return c.getInt("layer_num", 0)
}

// TotalLayerNum gets the total layer number.
func (c *PrinterMQTTClient) TotalLayerNum() int {
	return c.getInt("total_layer_num", 0)
}

// NozzleDiameter gets the nozzle diameter.
func (c *PrinterMQTTClient) NozzleDiameter() float64 {
	return c.getFloat64("nozzle_diameter", 0.4)
}

// NozzleType gets the nozzle type.
func (c *PrinterMQTTClient) NozzleType() printerinfo.NozzleType {
	nozzleType := c.getString("nozzle_type", "stainless_steel")
	return printerinfo.ParseNozzleType(nozzleType)
}

// GetFanGear gets the consolidated fan value.
func (c *PrinterMQTTClient) GetFanGear() int {
	return c.getInt("fan_gear", 0)
}

// GetPartFanSpeed gets the part fan speed.
func (c *PrinterMQTTClient) GetPartFanSpeed() int {
	return c.GetFanGear() % 256
}

// GetAuxFanSpeed gets the auxiliary fan speed.
func (c *PrinterMQTTClient) GetAuxFanSpeed() int {
	return (c.GetFanGear() >> 8) % 256
}

// GetChamberFanSpeed gets the chamber fan speed.
func (c *PrinterMQTTClient) GetChamberFanSpeed() int {
	return c.GetFanGear() >> 16
}

// Dump returns the current data dump.
func (c *PrinterMQTTClient) Dump() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]interface{})
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// setTemperatureSupport checks if the printer supports direct temperature commands.
func (c *PrinterMQTTClient) setTemperatureSupport() bool {
	printerType := c.printerInfo.PrinterType
	firmwareVersion := c.printerInfo.FirmwareVersion

	if printerType == printerinfo.PrinterTypeP1P || printerType == printerinfo.PrinterTypeP1S ||
		printerType == printerinfo.PrinterTypeX1E || printerType == printerinfo.PrinterTypeX1C {
		return compareVersions(firmwareVersion, "01.06") < 0
	} else if printerType == printerinfo.PrinterTypeA1 || printerType == printerinfo.PrinterTypeA1Mini {
		return compareVersions(firmwareVersion, "01.04") <= 0
	}
	return false
}

// compareVersions compares two version strings.
func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(strings.ReplaceAll(v1, " ", ""), ".")
	parts2 := strings.Split(strings.ReplaceAll(v2, " ", ""), ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		n1, _ := strconv.Atoi(parts1[i])
		n2, _ := strconv.Atoi(parts2[i])
		if n1 > n2 {
			return 1
		} else if n1 < n2 {
			return -1
		}
	}

	if len(parts1) > len(parts2) {
		return 1
	} else if len(parts1) < len(parts2) {
		return -1
	}
	return 0
}

// isValidGcode checks if a line is a valid G-code command.
func isValidGcode(line string) bool {
	// Remove comments
	parts := strings.Split(line, ";")
	line = strings.TrimSpace(parts[0])

	if line == "" {
		return false
	}

	// Check if starts with G or M
	if !strings.HasPrefix(line, "G") && !strings.HasPrefix(line, "M") {
		return false
	}

	// Check for proper parameter formatting
	tokens := strings.Fields(line)
	paramRegex := regexp.MustCompile(`^[A-Z]-?\d+(\.\d+)?$`)

	for i, token := range tokens {
		if i == 0 {
			if !regexp.MustCompile(`^[GM]\d+$`).MatchString(token) {
				return false
			}
		} else {
			if !paramRegex.MatchString(token) {
				return false
			}
		}
	}

	return true
}

// sendGcodeLine sends a single G-code line.
func (c *PrinterMQTTClient) sendGcodeLine(gcodeCommand string) bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"sequence_id": "0",
			"command":     "gcode_line",
			"param":       gcodeCommand,
		},
	})
}

// SendGcode sends G-code command(s) to the printer.
func (c *PrinterMQTTClient) SendGcode(gcodeCommand interface{}, gcodeCheck bool) (bool, error) {
	switch cmd := gcodeCommand.(type) {
	case string:
		if gcodeCheck && !isValidGcode(cmd) {
			return false, fmt.Errorf("invalid G-code command: %s", cmd)
		}
		return c.sendGcodeLine(cmd), nil
	case []string:
		if gcodeCheck {
			for _, g := range cmd {
				if !isValidGcode(g) {
					return false, fmt.Errorf("invalid G-code command: %s", g)
				}
			}
		}
		return c.sendGcodeLine(strings.Join(cmd, "\n")), nil
	default:
		return false, fmt.Errorf("invalid gcode command type")
	}
}

// SetBedTemperature sets the bed temperature.
func (c *PrinterMQTTClient) SetBedTemperature(temperature int, override bool) bool {
	if c.setTemperatureSupport() {
		return c.sendGcodeLine(fmt.Sprintf("M140 S%d", temperature))
	}

	if temperature < 40 && !override {
		warnLog.Printf("Attempting to set low bed temperature (%d). Use override=true to force.", temperature)
		return false
	}
	return c.sendGcodeLine(fmt.Sprintf("M190 S%d", temperature))
}

// SetNozzleTemperature sets the nozzle temperature.
func (c *PrinterMQTTClient) SetNozzleTemperature(temperature int, override bool) bool {
	if c.setTemperatureSupport() {
		return c.sendGcodeLine(fmt.Sprintf("M104 S%d", temperature))
	}

	if temperature < 60 && !override {
		warnLog.Printf("Attempting to set low nozzle temperature (%d). Use override=true to force.", temperature)
		return false
	}
	return c.sendGcodeLine(fmt.Sprintf("M109 S%d", temperature))
}

// SetPrintSpeedLevel sets the print speed level (0-3).
func (c *PrinterMQTTClient) SetPrintSpeedLevel(speedLevel int) bool {
	if speedLevel < 0 || speedLevel > 3 {
		errorLog.Printf("Invalid speed level: %d (must be 0-3)", speedLevel)
		return false
	}

	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "print_speed",
			"param":   fmt.Sprintf("%d", speedLevel),
		},
	})
}

// SetPartFanSpeed sets the part fan speed (0-255 or 0.0-1.0).
func (c *PrinterMQTTClient) SetPartFanSpeed(speed interface{}) (bool, error) {
	return c.setFanSpeed(speed, 1)
}

// SetAuxFanSpeed sets the auxiliary fan speed (0-255 or 0.0-1.0).
func (c *PrinterMQTTClient) SetAuxFanSpeed(speed interface{}) (bool, error) {
	return c.setFanSpeed(speed, 2)
}

// SetChamberFanSpeed sets the chamber fan speed (0-255 or 0.0-1.0).
func (c *PrinterMQTTClient) SetChamberFanSpeed(speed interface{}) (bool, error) {
	return c.setFanSpeed(speed, 3)
}

// setFanSpeed sets a fan speed.
func (c *PrinterMQTTClient) setFanSpeed(speed interface{}, fanNum int) (bool, error) {
	var speedInt int

	switch s := speed.(type) {
	case int:
		if s < 0 || s > 255 {
			return false, fmt.Errorf("fan speed %d is not between 0 and 255", s)
		}
		speedInt = s
	case float64:
		if s < 0 || s > 1 {
			return false, fmt.Errorf("fan speed %f is not between 0 and 1", s)
		}
		speedInt = int(255 * s)
	default:
		return false, fmt.Errorf("fan speed must be int or float")
	}

	return c.sendGcodeLine(fmt.Sprintf("M106 P%d S%d", fanNum, speedInt)), nil
}

// AutoHome homes the printer.
func (c *PrinterMQTTClient) AutoHome() bool {
	return c.sendGcodeLine("G28")
}

// SetBedHeight sets the Z-axis height.
func (c *PrinterMQTTClient) SetBedHeight(height int) bool {
	return c.sendGcodeLine(fmt.Sprintf("G90\nG0 Z%d", height))
}

// SetPartFanSpeedInt sets the part fan speed (0-255).
func (c *PrinterMQTTClient) SetPartFanSpeedInt(speed int) bool {
	_, err := c.SetPartFanSpeed(speed)
	if err != nil {
		errorLog.Printf("Failed to set part fan speed: %v", err)
		return false
	}
	return true
}

// SetAuxFanSpeedInt sets the aux fan speed (0-255).
func (c *PrinterMQTTClient) SetAuxFanSpeedInt(speed int) bool {
	_, err := c.SetAuxFanSpeed(speed)
	if err != nil {
		errorLog.Printf("Failed to set aux fan speed: %v", err)
		return false
	}
	return true
}

// SetChamberFanSpeedInt sets the chamber fan speed (0-255).
func (c *PrinterMQTTClient) SetChamberFanSpeedInt(speed int) bool {
	_, err := c.SetChamberFanSpeed(speed)
	if err != nil {
		errorLog.Printf("Failed to set chamber fan speed: %v", err)
		return false
	}
	return true
}

// SetAutoStepRecovery sets auto step recovery.
func (c *PrinterMQTTClient) SetAutoStepRecovery(autoStepRecovery bool) bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command":       "gcode_line",
			"auto_recovery": autoStepRecovery,
		},
	})
}

// StartPrint3MF starts printing a 3MF file.
func (c *PrinterMQTTClient) StartPrint3MF(filename string, plateNumber interface{}, useAMS bool, amsMapping []int, skipObjects []int, flowCalibration bool) bool {
	var plateLocation string

	switch pn := plateNumber.(type) {
	case int:
		plateLocation = fmt.Sprintf("Metadata/plate_%d.gcode", pn)
	case string:
		plateLocation = pn
	default:
		plateLocation = "Metadata/plate_1.gcode"
	}

	payload := map[string]interface{}{
		"print": map[string]interface{}{
			"command":        "project_file",
			"param":          plateLocation,
			"file":           filename,
			"bed_leveling":   true,
			"bed_type":       "textured_plate",
			"flow_cali":      flowCalibration,
			"vibration_cali": true,
			"url":            fmt.Sprintf("ftp:///%s", filename),
			"layer_inspect":  false,
			"sequence_id":    "10000000",
			"use_ams":        useAMS,
			"ams_mapping":    amsMapping,
		},
	}

	if skipObjects != nil && len(skipObjects) > 0 {
		payload["print"].(map[string]interface{})["skip_objects"] = skipObjects
	}

	return c.publishCommand(payload)
}

// StopPrint stops the current print.
func (c *PrinterMQTTClient) StopPrint() bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "stop",
		},
	})
}

// PausePrint pauses the current print.
func (c *PrinterMQTTClient) PausePrint() bool {
	if c.GetPrinterState() == states.GcodeStatePause {
		return true
	}
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "pause",
		},
	})
}

// ResumePrint resumes a paused print.
func (c *PrinterMQTTClient) ResumePrint() bool {
	if c.GetPrinterState() == states.GcodeStateRunning {
		return true
	}
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "resume",
		},
	})
}

// SkipObjects skips objects during printing.
func (c *PrinterMQTTClient) SkipObjects(objList []int) bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command":  "skip_objects",
			"obj_list": objList,
		},
	})
}

// GetSkippedObjects gets the list of skipped objects.
func (c *PrinterMQTTClient) GetSkippedObjects() []int {
	objs := c.getPrintValue("s_obj", []interface{}{})
	result := []int{}

	if objList, ok := objs.([]interface{}); ok {
		for _, o := range objList {
			switch v := o.(type) {
			case float64:
				result = append(result, int(v))
			case int:
				result = append(result, v)
			case int64:
				result = append(result, int(v))
			case string:
				if parsed, err := strconv.Atoi(v); err == nil {
					result = append(result, parsed)
				}
			}
		}
	}
	return result
}

// SetPrinterFilament sets the printer filament settings.
func (c *PrinterMQTTClient) SetPrinterFilament(filament filament.AMSFilamentSettings, color string, amsID, trayID int) bool {
	if len(color) != 6 {
		errorLog.Println("Color must be a 6 character hex code")
		return false
	}

	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command":         "ams_filament_setting",
			"ams_id":          amsID,
			"tray_id":         trayID,
			"tray_info_idx":   filament.TrayInfoIdx,
			"tray_color":      strings.ToUpper(color) + "FF",
			"nozzle_temp_min": filament.NozzleTempMin,
			"nozzle_temp_max": filament.NozzleTempMax,
			"tray_type":       filament.TrayType,
		},
	})
}

// LoadFilamentSpool loads filament from the spool.
func (c *PrinterMQTTClient) LoadFilamentSpool() bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command":   "ams_change_filament",
			"target":    255,
			"curr_temp": 215,
			"tar_temp":  215,
		},
	})
}

// UnloadFilamentSpool unloads filament from the spool.
func (c *PrinterMQTTClient) UnloadFilamentSpool() bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command":   "ams_change_filament",
			"target":    254,
			"curr_temp": 215,
			"tar_temp":  215,
		},
	})
}

// ResumeFilamentAction resumes the filament action.
func (c *PrinterMQTTClient) ResumeFilamentAction() bool {
	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "ams_control",
			"param":   "resume",
		},
	})
}

// Calibration starts printer calibration.
func (c *PrinterMQTTClient) Calibration(bedLeveling, motorNoiseCancellation, vibrationCompensation bool) bool {
	bitmask := 0

	if bedLeveling {
		bitmask |= 1 << 1
	}
	if vibrationCompensation {
		bitmask |= 1 << 2
	}
	if motorNoiseCancellation {
		bitmask |= 1 << 3
	}

	return c.publishCommand(map[string]interface{}{
		"print": map[string]interface{}{
			"command": "calibration",
			"option":  bitmask,
		},
	})
}

// SetOnboardPrinterTimelapse enables/disables onboard timelapse.
func (c *PrinterMQTTClient) SetOnboardPrinterTimelapse(enable bool) bool {
	control := "enable"
	if !enable {
		control = "disable"
	}

	return c.publishCommand(map[string]interface{}{
		"camera": map[string]interface{}{
			"command": "ipcam_record_set",
			"control": control,
		},
	})
}

// SetNozzleInfo sets the nozzle information.
func (c *PrinterMQTTClient) SetNozzleInfo(nozzleType printerinfo.NozzleType, nozzleDiameter float64) bool {
	return c.publishCommand(map[string]interface{}{
		"system": map[string]interface{}{
			"accessory_type":  "nozzle",
			"command":         "set_accessories",
			"nozzle_diameter": nozzleDiameter,
			"nozzle_type":     nozzleType.String(),
		},
	})
}

// NewPrinterFirmware checks if new firmware is available.
func (c *PrinterMQTTClient) NewPrinterFirmware() string {
	upgradeState, ok := c.getPrintValue("upgrade_state", nil).(map[string]interface{})
	if !ok {
		return ""
	}

	newVerList, ok := upgradeState["new_ver_list"].([]interface{})
	if !ok {
		return ""
	}

	for _, item := range newVerList {
		if i, ok := item.(map[string]interface{}); ok {
			if name, ok := i["name"].(string); ok && name == "ota" {
				if ver, ok := i["new_ver"].(string); ok {
					return ver
				}
			}
		}
	}
	return ""
}

// UpgradeFirmware upgrades to the latest firmware.
func (c *PrinterMQTTClient) UpgradeFirmware(override bool) bool {
	newFirmware := c.NewPrinterFirmware()
	if newFirmware == "" {
		return false
	}

	if compareVersions(newFirmware, "1.08") >= 0 && !override {
		warnLog.Printf("Firmware %s may cause API incompatibility. Use override=true to force.", newFirmware)
		return false
	}

	return c.publishCommand(map[string]interface{}{
		"upgrade": map[string]interface{}{
			"command": "upgrade_confirm",
			"src_id":  2,
		},
	})
}

// ProcessAMS processes AMS information from the data.
func (c *PrinterMQTTClient) ProcessAMS() {
	c.mu.RLock()
	printData, ok := c.data["print"].(map[string]interface{})
	c.mu.RUnlock()

	if !ok {
		return
	}

	amsInfo, ok := printData["ams"].(map[string]interface{})
	if !ok {
		return
	}

	c.amsHub = ams.NewAMSHub()

	amsExistBits, ok := amsInfo["ams_exist_bits"].(string)
	if !ok || amsExistBits == "0" {
		return
	}

	amsUnits, ok := amsInfo["ams"].([]interface{})
	if !ok {
		return
	}

	for k, amsUnit := range amsUnits {
		if amsData, ok := amsUnit.(map[string]interface{}); ok {
			// Parse humidity (can be string or float64)
			humidity := 0
			if h, ok := amsData["humidity"]; ok {
				switch v := h.(type) {
				case float64:
					humidity = int(v)
				case string:
					fmt.Sscanf(v, "%d", &humidity)
				case int:
					humidity = v
				}
			}
			// Parse temperature (can be string or float64)
			temp := 0.0
			if t, ok := amsData["temp"]; ok {
				switch v := t.(type) {
				case float64:
					temp = v
				case string:
					fmt.Sscanf(v, "%f", &temp)
				case int:
					temp = float64(v)
				}
			}
			// Parse ID (can be string or float64)
			id := k
			if i, ok := amsData["id"]; ok {
				switch v := i.(type) {
				case float64:
					id = int(v)
				case string:
					fmt.Sscanf(v, "%d", &id)
				case int:
					id = v
				}
			}

			amsUnit := ams.NewAMS(humidity, temp)

			if trays, ok := amsData["tray"].([]interface{}); ok {
				for _, tray := range trays {
					if trayData, ok := tray.(map[string]interface{}); ok {
						// Parse tray ID (can be string or float64)
						trayID := 0
						if tid, ok := trayData["id"]; ok {
							switch v := tid.(type) {
							case float64:
								trayID = int(v)
							case string:
								fmt.Sscanf(v, "%d", &trayID)
							case int:
								trayID = v
							}
						}
						if _, ok := trayData["n"]; ok {
							trayInfo := filament.FilamentTrayFromDict(trayData)
							amsUnit.SetFilamentTray(trayID, &trayInfo)
						}
					}
				}
			}

			c.amsHub.Set(id, amsUnit)
		}
	}
}

// AMSHub returns the AMS hub.
func (c *PrinterMQTTClient) AMSHub() *ams.AMSHub {
	return c.amsHub
}

// VTTray gets the external spool filament tray.
func (c *PrinterMQTTClient) VTTray() filament.FilamentTray {
	trayData := c.getPrintValue("vt_tray", nil)
	if trayData == nil {
		return filament.FilamentTray{}
	}

	if trayMap, ok := trayData.(map[string]interface{}); ok {
		return filament.FilamentTrayFromDict(trayMap)
	}
	return filament.FilamentTray{}
}

// SubtaskName gets the current subtask name.
func (c *PrinterMQTTClient) SubtaskName() string {
	return c.getString("subtask_name", "")
}

// GcodeFile gets the current gcode file name.
func (c *PrinterMQTTClient) GcodeFile() string {
	return c.getString("gcode_file", "")
}

// PrintErrorCode gets the print error code.
func (c *PrinterMQTTClient) PrintErrorCode() int {
	return c.getInt("print_error", 0)
}

// PrintType gets the print type (cloud/local).
func (c *PrinterMQTTClient) PrintType() string {
	return c.getString("print_type", "")
}

// WifiSignal gets the WiFi signal strength in dBm.
func (c *PrinterMQTTClient) WifiSignal() string {
	return c.getString("wifi_signal", "")
}

// Reboot reboots the printer.
func (c *PrinterMQTTClient) Reboot() bool {
	warnLog.Println("Sending reboot command!")
	return c.publishCommand(map[string]interface{}{
		"system": map[string]interface{}{
			"command": "reboot",
		},
	})
}

// GetAccessCode gets the access code.
func (c *PrinterMQTTClient) GetAccessCode() string {
	systemData := c.getInfoValue("system", nil)
	if systemData == nil {
		return c.access
	}

	if sysMap, ok := systemData.(map[string]interface{}); ok {
		if code, ok := sysMap["command"].(string); ok {
			if code != c.access {
				errorLog.Printf("Unexpected access code: expected %s, got %s", c.access, code)
			}
			return code
		}
	}
	return c.access
}

// RequestAccessCode requests the access code from the printer.
func (c *PrinterMQTTClient) RequestAccessCode() bool {
	return c.publishCommand(map[string]interface{}{
		"system": map[string]interface{}{
			"command": "get_access_code",
		},
	})
}

// GetFirmwareHistory gets the firmware history.
func (c *PrinterMQTTClient) GetFirmwareHistory() []map[string]interface{} {
	upgradeData := c.getInfoValue("upgrade", nil)
	if upgradeData == nil {
		return []map[string]interface{}{}
	}

	if upgradeMap, ok := upgradeData.(map[string]interface{}); ok {
		if history, ok := upgradeMap["firmware_optional"].([]interface{}); ok {
			result := make([]map[string]interface{}, len(history))
			for i, h := range history {
				if hm, ok := h.(map[string]interface{}); ok {
					result[i] = hm
				}
			}
			return result
		}
	}
	return []map[string]interface{}{}
}

// DowngradeFirmware downgrades to a specific firmware version.
func (c *PrinterMQTTClient) DowngradeFirmware(firmwareVersion string) bool {
	firmwareHistory := c.GetFirmwareHistory()
	if len(firmwareHistory) == 0 {
		warnLog.Println("Firmware history not up to date")
		return false
	}

	var targetFirmware map[string]interface{}
	for _, firmware := range firmwareHistory {
		if fwData, ok := firmware["firmware"].(map[string]interface{}); ok {
			if version, ok := fwData["version"].(string); ok && version == firmwareVersion {
				targetFirmware = firmware
				break
			}
		}
	}

	if targetFirmware == nil {
		warnLog.Printf("Firmware %s not found in listed firmware", firmwareVersion)
		return false
	}

	return c.publishCommand(map[string]interface{}{
		"upgrade": map[string]interface{}{
			"command":           "upgrade_history",
			"src_id":            2,
			"firmware_optional": targetFirmware,
		},
	})
}
