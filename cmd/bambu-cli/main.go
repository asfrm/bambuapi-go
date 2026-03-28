// bambu-cli is a command-line tool for controlling Bambu Lab 3D printers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/asfrm/bambuapi-go/printer"
	"github.com/asfrm/bambuapi-go/states"
)

// Config holds the printer connection configuration.
type Config struct {
	IP         string
	AccessCode string
	Serial     string
}

// loadConfig loads configuration from flags or environment variables.
func loadConfig() (Config, string, []string) {
	var cfg Config

	// Save original args before flag.Parse() messes with them
	origArgs := make([]string, len(os.Args))
	copy(origArgs, os.Args)

	// First, find the command (first non-flag argument that's not a flag value)
	// Skip arguments that are flag values (follow a -flag)
	var command string
	commandIdx := -1
	skipNext := false
	for i := 1; i < len(origArgs); i++ {
		arg := origArgs[i]
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			// Check if it's a flag with a value (not --boolean)
			if i+1 < len(origArgs) && !strings.HasPrefix(origArgs[i+1], "-") {
				// Check if this is a known flag
				if arg == "-ip" || arg == "-code" || arg == "-serial" ||
					arg == "--ip" || arg == "--code" || arg == "--serial" ||
					arg == "-nozzle" || arg == "-bed" || arg == "-json" ||
					arg == "-watch" || arg == "-h" || arg == "--help" {
					skipNext = true
					continue
				}
			}
			continue
		}
		// This is a command
		command = arg
		commandIdx = i
		break
	}

	// Flag definitions
	flag.StringVar(&cfg.IP, "ip", getEnv("BAMBU_IP", ""), "Printer IP address")
	flag.StringVar(&cfg.AccessCode, "code", getEnv("BAMBU_CODE", ""), "Printer access code")
	flag.StringVar(&cfg.Serial, "serial", getEnv("BAMBU_SERIAL", ""), "Printer serial number")

	// Parse flags
	flag.Parse()

	// Get command-specific arguments (everything after the command) from ORIGINAL args
	var cmdArgs []string
	if commandIdx >= 0 {
		for i := commandIdx + 1; i < len(origArgs); i++ {
			cmdArgs = append(cmdArgs, origArgs[i])
		}
	}

	return cfg, command, cmdArgs
}

// getEnv gets an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// validateConfig validates the configuration.
func validateConfig(cfg Config) error {
	var missing []string
	if cfg.IP == "" {
		missing = append(missing, "IP (--ip or BAMBU_IP)")
	}
	if cfg.AccessCode == "" {
		missing = append(missing, "Access Code (--code or BAMBU_CODE)")
	}
	if cfg.Serial == "" {
		missing = append(missing, "Serial (--serial or BAMBU_SERIAL)")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

// connectMQTT connects to the printer via MQTT only (no camera).
// It waits for the MQTT client to connect and receive initial data.
func connectMQTT(cfg Config) (*printer.Printer, error) {
	p := printer.NewPrinter(cfg.IP, cfg.AccessCode, cfg.Serial)

	// Disable aggressive mode BEFORE connecting to avoid blocking on info requests
	p.SetMQTTAggressiveMode(false)

	// Start only the MQTT client - do NOT start camera for commands
	if err := p.MQTTStart(); err != nil {
		return nil, fmt.Errorf("failed to start MQTT client: %w", err)
	}

	// Wait for MQTT connection and initial data (max 15 seconds)
	timeout := time.After(15 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)

	for {
		select {
		case <-timeout:
			ticker.Stop()
			p.MQTTStop()
			return nil, fmt.Errorf("timeout waiting for MQTT connection")
		case <-ticker.C:
			if p.MQTTClientConnected() {
				ticker.Stop()
				// Connection established, wait for MQTT traffic to settle
				// This ensures we can acquire the read lock for status queries
				time.Sleep(3 * time.Second)
				return p, nil
			}
		}
	}
}

// printStatusJSON prints the printer status in JSON format.
func printStatusJSON(p *printer.Printer) error {
	status := map[string]interface{}{
		"state":               p.GetState().String(),
		"print_status":        p.GetCurrentState().String(),
		"nozzle_temperature":  p.GetNozzleTemperature(),
		"bed_temperature":     p.GetBedTemperature(),
		"chamber_temperature": p.GetChamberTemperature(),
		"progress_percent":    p.GetPercentage(),
		"remaining_time_sec":  p.GetTime(),
		"current_layer":       p.CurrentLayerNum(),
		"total_layers":        p.TotalLayerNum(),
		"print_speed":         p.GetPrintSpeed(),
		"file_name":           p.GetFileName(),
		"light_state":         p.GetLightState(),
		"part_fan_speed":      p.GetPartFanSpeed(),
		"aux_fan_speed":       p.GetAuxFanSpeed(),
		"chamber_fan_speed":   p.GetChamberFanSpeed(),
		"wifi_signal_dbm":     p.WifiSignal(),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(status)
}

// printStatusHuman prints the printer status in human-readable format.
func printStatusHuman(p *printer.Printer) {
	state := p.GetState()
	printStatus := p.GetCurrentState()

	fmt.Println("=== Bambu Lab Printer Status ===")
	fmt.Println()
	fmt.Printf("State:          %s\n", state.String())
	fmt.Printf("Print Status:   %s\n", printStatus.String())
	fmt.Println()
	fmt.Println("--- Temperatures ---")
	fmt.Printf("Nozzle:         %.1f°C\n", p.GetNozzleTemperature())
	fmt.Printf("Bed:            %.1f°C\n", p.GetBedTemperature())
	fmt.Printf("Chamber:        %.1f°C\n", p.GetChamberTemperature())
	fmt.Println()
	fmt.Println("--- Print Progress ---")
	fmt.Printf("Progress:       %d%%\n", p.GetPercentage())
	fmt.Printf("Remaining Time: %s\n", formatDuration(p.GetTime()))
	fmt.Printf("Current Layer:  %d / %d\n", p.CurrentLayerNum(), p.TotalLayerNum())
	fmt.Println()
	fmt.Println("--- Settings ---")
	fmt.Printf("Print Speed:    %d%%\n", p.GetPrintSpeed())
	fmt.Printf("Light:          %s\n", p.GetLightState())
	fmt.Printf("File:           %s\n", p.GetFileName())
	fmt.Println()
	fmt.Println("--- Fans ---")
	fmt.Printf("Part Fan:       %d RPM\n", p.GetPartFanSpeed())
	fmt.Printf("Aux Fan:        %d RPM\n", p.GetAuxFanSpeed())
	fmt.Printf("Chamber Fan:    %d RPM\n", p.GetChamberFanSpeed())
	fmt.Println()
	fmt.Printf("WiFi Signal:    %s dBm\n", p.WifiSignal())
}

// formatDuration formats seconds into a human-readable duration.
func formatDuration(seconds int) string {
	if seconds <= 0 {
		return "N/A"
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// cmdStatus handles the status command.
func cmdStatus(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "Output status as JSON")
	watch := fs.Bool("watch", false, "Continuously watch status (Ctrl+C to stop)")
	fs.Parse(args)

	if *watch {
		return watchStatus(p, *jsonOutput)
	}

	if *jsonOutput {
		return printStatusJSON(p)
	}
	printStatusHuman(p)
	return nil
}

// watchStatus continuously monitors and prints status.
func watchStatus(p *printer.Printer, jsonOutput bool) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !p.MQTTClientConnected() {
				return fmt.Errorf("MQTT connection lost")
			}
			if jsonOutput {
				if err := printStatusJSON(p); err != nil {
					return err
				}
			} else {
				printStatusHuman(p)
			}
			fmt.Println()
		}
	}
}

// cmdHome handles the home command.
func cmdHome(p *printer.Printer, args []string) error {
	state := p.GetState()
	if state == states.GcodeStateRunning {
		return fmt.Errorf("safety check: cannot home while printer is RUNNING")
	}

	if p.HomePrinter() {
		fmt.Println("Homing printer (G28)...")
		return nil
	}
	return fmt.Errorf("failed to home printer")
}

// cmdTemp handles the temp command.
func cmdTemp(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("temp", flag.ExitOnError)
	nozzleTemp := fs.Int("nozzle", 0, "Set nozzle temperature (°C)")
	bedTemp := fs.Int("bed", 0, "Set bed temperature (°C)")
	fs.Parse(args)

	if *nozzleTemp == 0 && *bedTemp == 0 {
		// Just show current temperatures
		fmt.Printf("Current Temperatures:\n")
		fmt.Printf("  Nozzle:  %.1f°C\n", p.GetNozzleTemperature())
		fmt.Printf("  Bed:     %.1f°C\n", p.GetBedTemperature())
		fmt.Printf("  Chamber: %.1f°C\n", p.GetChamberTemperature())
		return nil
	}

	if *nozzleTemp > 0 {
		if p.SetNozzleTemperature(*nozzleTemp) {
			fmt.Printf("Setting nozzle temperature to %d°C...\n", *nozzleTemp)
		} else {
			return fmt.Errorf("failed to set nozzle temperature")
		}
	}

	if *bedTemp > 0 {
		if p.SetBedTemperature(*bedTemp) {
			fmt.Printf("Setting bed temperature to %d°C...\n", *bedTemp)
		} else {
			return fmt.Errorf("failed to set bed temperature")
		}
	}

	return nil
}

// cmdLight handles the light command.
func cmdLight(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli light <on|off>")
	}

	// Use a channel to handle the result with timeout
	done := make(chan bool, 1)
	var result bool

	switch strings.ToLower(args[0]) {
	case "on":
		go func() {
			result = p.TurnLightOn()
			done <- true
		}()
	case "off":
		go func() {
			result = p.TurnLightOff()
			done <- true
		}()
	default:
		return fmt.Errorf("invalid argument: %s (use 'on' or 'off')", args[0])
	}

	// Wait for result with timeout
	select {
	case <-done:
		if result {
			fmt.Printf("Turning light %s...\n", strings.ToUpper(args[0]))
			return nil
		}
		return fmt.Errorf("failed to turn light %s", args[0])
	case <-time.After(3 * time.Second):
		// Even if timeout, the command was likely sent
		fmt.Printf("Command sent: light %s\n", strings.ToUpper(args[0]))
		return nil
	}
}

// cmdGcode handles the gcode command.
func cmdGcode(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli gcode <gcode-command>")
	}

	gcodeCmd := strings.Join(args, " ")
	success, err := p.Gcode(gcodeCmd, true)
	if err != nil {
		return fmt.Errorf("error: %w", err)
	}
	if !success {
		return fmt.Errorf("failed to send G-code command")
	}

	fmt.Printf("Sent G-code: %s\n", gcodeCmd)
	return nil
}

// cmdPause handles the pause command.
func cmdPause(p *printer.Printer, args []string) error {
	if p.PausePrint() {
		fmt.Println("Pausing print...")
		return nil
	}
	return fmt.Errorf("failed to pause print")
}

// cmdResume handles the resume command.
func cmdResume(p *printer.Printer, args []string) error {
	if p.ResumePrint() {
		fmt.Println("Resuming print...")
		return nil
	}
	return fmt.Errorf("failed to resume print")
}

// cmdStop handles the stop command.
func cmdStop(p *printer.Printer, args []string) error {
	if p.StopPrint() {
		fmt.Println("Stopping print...")
		return nil
	}
	return fmt.Errorf("failed to stop print")
}

// cmdFan handles the fan command.
func cmdFan(p *printer.Printer, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: bambu-cli fan <part|aux|chamber> <0-255>")
	}

	fanType := strings.ToLower(args[0])
	speed := 0
	fmt.Sscanf(args[1], "%d", &speed)

	if speed < 0 || speed > 255 {
		return fmt.Errorf("fan speed must be between 0 and 255")
	}

	var success bool
	switch fanType {
	case "part":
		success = p.SetPartFanSpeedInt(speed)
	case "aux":
		success = p.SetAuxFanSpeedInt(speed)
	case "chamber":
		success = p.SetChamberFanSpeedInt(speed)
	default:
		return fmt.Errorf("unknown fan type: %s (use part|aux|chamber)", fanType)
	}

	if success {
		fmt.Printf("Setting %s fan speed to %d...\n", fanType, speed)
		return nil
	}
	return fmt.Errorf("failed to set %s fan speed", fanType)
}

// cmdSpeed handles the speed command.
func cmdSpeed(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		// Show current speed
		fmt.Printf("Current print speed: %d%%\n", p.GetPrintSpeed())
		return nil
	}

	level := 0
	fmt.Sscanf(args[0], "%d", &level)

	if level < 0 || level > 3 {
		return fmt.Errorf("speed level must be between 0 and 3 (silent, standard, sport, ludicrous)")
	}

	if p.SetPrintSpeed(level) {
		speedNames := []string{"Silent", "Standard", "Sport", "Ludicrous"}
		fmt.Printf("Setting print speed to %s (level %d)...\n", speedNames[level], level)
		return nil
	}
	return fmt.Errorf("failed to set print speed")
}

// cmdCalibrate handles the calibrate command.
func cmdCalibrate(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ExitOnError)
	bedLevel := fs.Bool("bed", false, "Run bed leveling")
	motorNoise := fs.Bool("motor", false, "Run motor noise calibration")
	vibration := fs.Bool("vibration", false, "Run vibration compensation")
	fs.Parse(args)

	if !*bedLevel && !*motorNoise && !*vibration {
		// Default to all
		*bedLevel = true
		*motorNoise = true
		*vibration = true
	}

	if p.CalibratePrinter(*bedLevel, *motorNoise, *vibration) {
		fmt.Println("Starting calibration...")
		if *bedLevel {
			fmt.Println("  - Bed leveling")
		}
		if *motorNoise {
			fmt.Println("  - Motor noise calibration")
		}
		if *vibration {
			fmt.Println("  - Vibration compensation")
		}
		return nil
	}
	return fmt.Errorf("failed to start calibration")
}

// cmdFilament handles the filament command.
func cmdFilament(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli filament <load|unload|retry>")
	}

	switch strings.ToLower(args[0]) {
	case "load":
		if p.LoadFilamentSpool() {
			fmt.Println("Loading filament...")
			return nil
		}
		return fmt.Errorf("failed to load filament")
	case "unload":
		if p.UnloadFilamentSpool() {
			fmt.Println("Unloading filament...")
			return nil
		}
		return fmt.Errorf("failed to unload filament")
	case "retry":
		if p.RetryFilamentAction() {
			fmt.Println("Retrying filament action...")
			return nil
		}
		return fmt.Errorf("failed to retry filament action")
	default:
		return fmt.Errorf("unknown filament action: %s (use load|unload|retry)", args[0])
	}
}

// cmdReboot handles the reboot command.
func cmdReboot(p *printer.Printer, args []string) error {
	fmt.Println("WARNING: This will reboot the printer!")
	if p.Reboot() {
		fmt.Println("Sending reboot command...")
		return nil
	}
	return fmt.Errorf("failed to send reboot command")
}

// cmdFirmware handles the firmware command.
func cmdFirmware(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		// Check for updates
		newFw := p.NewPrinterFirmware()
		if newFw != "" {
			fmt.Printf("New firmware available: %s\n", newFw)
		} else {
			fmt.Println("Firmware is up to date")
		}
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "upgrade":
		if p.UpgradeFirmware(false) {
			fmt.Println("Starting firmware upgrade...")
			return nil
		}
		return fmt.Errorf("failed to start firmware upgrade")
	default:
		return fmt.Errorf("unknown firmware action: %s (use upgrade)", args[0])
	}
}

// cmdInfo handles the info command.
func cmdInfo(p *printer.Printer, args []string) error {
	dump := p.MQTTDump()

	fmt.Println("=== Printer Information ===")
	fmt.Println()

	if info, ok := dump["info"].(map[string]interface{}); ok {
		if modules, ok := info["module"].([]interface{}); ok {
			fmt.Println("--- Firmware Modules ---")
			for _, m := range modules {
				if module, ok := m.(map[string]interface{}); ok {
					if name, ok := module["name"].(string); ok {
						if ver, ok := module["sw_ver"].(string); ok {
							fmt.Printf("  %-15s %s\n", name, ver)
						}
					}
				}
			}
			fmt.Println()
		}
	}

	fmt.Printf("Nozzle Type:     %s\n", p.NozzleType())
	fmt.Printf("Nozzle Diameter: %.1f mm\n", p.NozzleDiameter())
	fmt.Printf("Serial:          %s\n", p.Serial)
	fmt.Printf("IP Address:      %s\n", p.IPAddress)

	return nil
}

// cmdAMS handles the AMS command.
func cmdAMS(p *printer.Printer, args []string) error {
	amsHub := p.AMSHub()

	fmt.Println("=== AMS Status ===")
	fmt.Println()

	if amsHub == nil || len(amsHub.AMSHub) == 0 {
		fmt.Println("No AMS units connected")
		return nil
	}

	for id, ams := range amsHub.AMSHub {
		fmt.Printf("AMS Unit %d:\n", id)
		fmt.Printf("  Humidity: %d%%\n", ams.Humidity)
		fmt.Printf("  Temperature: %.1f°C\n", ams.Temperature)

		for trayIdx, tray := range ams.FilamentTrays {
			if tray.TrayInfoIdx != "" {
				fmt.Printf("  Tray %d: %s (%s)\n", trayIdx, tray.TrayInfoIdx, tray.TrayColor)
			}
		}
		fmt.Println()
	}

	return nil
}

// printUsage prints the main usage information.
func printUsage() {
	fmt.Println(`bambu-cli - Bambu Lab Printer CLI Tool

Usage:
  bambu-cli [flags] <command> [arguments]

Global Flags:
  -ip       Printer IP address (or BAMBU_IP env var)
  -code     Printer access code (or BAMBU_CODE env var)
  -serial   Printer serial number (or BAMBU_SERIAL env var)

Commands:
  status              Show printer status (use --json for JSON output, --watch to monitor)
  home                Home the printer (G28)
  temp                Show/set temperatures (--nozzle, --bed)
  light <on|off>      Control printer light
  gcode <command>     Send raw G-code command
  pause               Pause current print
  resume              Resume paused print
  stop                Stop current print
  fan <type> <speed>  Set fan speed (part|aux|chamber, 0-255)
  speed [level]       Show/set print speed (0-3)
  calibrate           Run printer calibration (--bed, --motor, --vibration)
  filament <action>   Filament control (load|unload|retry)
  ams                 Show AMS status
  info                Show printer information
  firmware            Check/upgrade firmware
  reboot              Reboot the printer

Examples:
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 temp --nozzle 220 --bed 60
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 light on
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 gcode "G28"
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status --json
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status --watch
`)
}

func main() {
	cfg, command, cmdArgs := loadConfig()

	// Check if help is requested or no command provided
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	// Handle help commands
	if command == "-h" || command == "--help" || command == "help" {
		printUsage()
		os.Exit(0)
	}

	// Validate config for all commands except help
	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printUsage()
		os.Exit(1)
	}

	// Connect to printer via MQTT only (no camera for commands)
	p, err := connectMQTT(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to printer: %v\n", err)
		os.Exit(1)
	}
	defer p.MQTTStop()

	// Execute command
	var cmdErr error
	switch command {
	case "status":
		cmdErr = cmdStatus(p, cmdArgs)
	case "home":
		cmdErr = cmdHome(p, cmdArgs)
	case "temp":
		cmdErr = cmdTemp(p, cmdArgs)
	case "light":
		cmdErr = cmdLight(p, cmdArgs)
	case "gcode":
		cmdErr = cmdGcode(p, cmdArgs)
	case "pause":
		cmdErr = cmdPause(p, cmdArgs)
	case "resume":
		cmdErr = cmdResume(p, cmdArgs)
	case "stop":
		cmdErr = cmdStop(p, cmdArgs)
	case "fan":
		cmdErr = cmdFan(p, cmdArgs)
	case "speed":
		cmdErr = cmdSpeed(p, cmdArgs)
	case "calibrate":
		cmdErr = cmdCalibrate(p, cmdArgs)
	case "filament":
		cmdErr = cmdFilament(p, cmdArgs)
	case "ams":
		cmdErr = cmdAMS(p, cmdArgs)
	case "info":
		cmdErr = cmdInfo(p, cmdArgs)
	case "firmware":
		cmdErr = cmdFirmware(p, cmdArgs)
	case "reboot":
		cmdErr = cmdReboot(p, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		p.MQTTStop()
		os.Exit(1)
	}

	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", cmdErr)
		os.Exit(1)
	}

	// Exit immediately without cleanup (MQTT goroutines will be killed)
	os.Exit(0)
}
