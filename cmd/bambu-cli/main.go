// bambu-cli is a command-line tool for controlling Bambu Lab 3D printers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/asfrm/bambusdk-go/filament"
	"github.com/asfrm/bambusdk-go/printer"
	"github.com/asfrm/bambusdk-go/states"
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
// The library's Connect() method handles full state request automatically.
func connectMQTT(cfg Config) (*printer.Printer, error) {
	p := printer.NewPrinter(cfg.IP, cfg.AccessCode, cfg.Serial)

	// Connect handles everything: MQTT connection, full state request, and waiting for data
	if err := p.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return p, nil
}

// printStatusJSON prints the printer status in JSON format.
func printStatusJSON(p *printer.Printer) error {
	status := map[string]any{
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
	fmt.Printf("WiFi Signal:    %s\n", p.WifiSignal())
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
	if err := fs.Parse(args); err != nil {
		return err
	}

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

	for range ticker.C {
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
	return nil
}

// cmdHome handles the home command.
func cmdHome(p *printer.Printer, args []string) error {
	state := p.GetState()
	if state == states.GcodeStateRunning {
		return fmt.Errorf("safety check: cannot home while printer is RUNNING")
	}

	if err := p.HomePrinter(); err != nil {
		return fmt.Errorf("failed to home printer: %w", err)
	}
	fmt.Println("Homing printer (G28)...")
	return nil
}

// cmdTemp handles the temp command.
func cmdTemp(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("temp", flag.ExitOnError)
	nozzleTemp := fs.Int("nozzle", 0, "Set nozzle temperature (°C)")
	bedTemp := fs.Int("bed", 0, "Set bed temperature (°C)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *nozzleTemp == 0 && *bedTemp == 0 {
		// Just show current temperatures
		fmt.Printf("Current Temperatures:\n")
		fmt.Printf("  Nozzle:  %.1f°C\n", p.GetNozzleTemperature())
		fmt.Printf("  Bed:     %.1f°C\n", p.GetBedTemperature())
		fmt.Printf("  Chamber: %.1f°C\n", p.GetChamberTemperature())
		return nil
	}

	if *nozzleTemp > 0 {
		if err := p.SetNozzleTemperature(*nozzleTemp); err != nil {
			return fmt.Errorf("failed to set nozzle temperature: %w", err)
		}
		fmt.Printf("Setting nozzle temperature to %d°C...\n", *nozzleTemp)
	}

	if *bedTemp > 0 {
		if err := p.SetBedTemperature(*bedTemp); err != nil {
			return fmt.Errorf("failed to set bed temperature: %w", err)
		}
		fmt.Printf("Setting bed temperature to %d°C...\n", *bedTemp)
	}

	return nil
}

// cmdLight handles the light command.
func cmdLight(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli light <on|off>")
	}

	var err error
	switch strings.ToLower(args[0]) {
	case "on":
		err = p.TurnLightOn()
	case "off":
		err = p.TurnLightOff()
	default:
		return fmt.Errorf("invalid argument: %s (use 'on' or 'off')", args[0])
	}

	if err != nil {
		return fmt.Errorf("failed to turn light %s: %w", args[0], err)
	}
	fmt.Printf("Turning light %s...\n", strings.ToUpper(args[0]))
	return nil
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
	if err := p.PausePrint(); err != nil {
		return fmt.Errorf("failed to pause print: %w", err)
	}
	fmt.Println("Pausing print...")
	return nil
}

// cmdResume handles the resume command.
func cmdResume(p *printer.Printer, args []string) error {
	if err := p.ResumePrint(); err != nil {
		return fmt.Errorf("failed to resume print: %w", err)
	}
	fmt.Println("Resuming print...")
	return nil
}

// cmdStop handles the stop command.
func cmdStop(p *printer.Printer, args []string) error {
	if err := p.StopPrint(); err != nil {
		return fmt.Errorf("failed to stop print: %w", err)
	}
	fmt.Println("Stopping print...")
	return nil
}

// cmdFan handles the fan command.
func cmdFan(p *printer.Printer, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: bambu-cli fan <part|aux|chamber> <0-255>")
	}

	fanType := strings.ToLower(args[0])
	speed := 0
	_, _ = fmt.Sscanf(args[1], "%d", &speed)

	if speed < 0 || speed > 255 {
		return fmt.Errorf("fan speed must be between 0 and 255")
	}

	var err error
	switch fanType {
	case "part":
		err = p.SetPartFanSpeedInt(speed)
	case "aux":
		err = p.SetAuxFanSpeedInt(speed)
	case "chamber":
		err = p.SetChamberFanSpeedInt(speed)
	default:
		return fmt.Errorf("unknown fan type: %s (use part|aux|chamber)", fanType)
	}

	if err != nil {
		return fmt.Errorf("failed to set %s fan speed: %w", fanType, err)
	}
	fmt.Printf("Setting %s fan speed to %d...\n", fanType, speed)
	return nil
}

// cmdSpeed handles the speed command.
func cmdSpeed(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		// Show current speed
		fmt.Printf("Current print speed: %d%%\n", p.GetPrintSpeed())
		return nil
	}

	level := 0
	_, _ = fmt.Sscanf(args[0], "%d", &level)

	if level < 0 || level > 3 {
		return fmt.Errorf("speed level must be between 0 and 3 (silent, standard, sport, ludicrous)")
	}

	if err := p.SetPrintSpeed(level); err != nil {
		return fmt.Errorf("failed to set print speed: %w", err)
	}
	speedNames := []string{"Silent", "Standard", "Sport", "Ludicrous"}
	fmt.Printf("Setting print speed to %s (level %d)...\n", speedNames[level], level)
	return nil
}

// cmdCalibrate handles the calibrate command.
func cmdCalibrate(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ExitOnError)
	bedLevel := fs.Bool("bed", false, "Run bed leveling")
	motorNoise := fs.Bool("motor", false, "Run motor noise calibration")
	vibration := fs.Bool("vibration", false, "Run vibration compensation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*bedLevel && !*motorNoise && !*vibration {
		// Default to all
		*bedLevel = true
		*motorNoise = true
		*vibration = true
	}

	if err := p.CalibratePrinter(*bedLevel, *motorNoise, *vibration); err != nil {
		return fmt.Errorf("failed to start calibration: %w", err)
	}
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

// cmdFilament handles the filament command.
func cmdFilament(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli filament <load|unload|retry>")
	}

	switch strings.ToLower(args[0]) {
	case "load":
		if err := p.LoadFilamentSpool(); err != nil {
			return fmt.Errorf("failed to load filament: %w", err)
		}
		fmt.Println("Loading filament...")
		return nil
	case "unload":
		if err := p.UnloadFilamentSpool(); err != nil {
			return fmt.Errorf("failed to unload filament: %w", err)
		}
		fmt.Println("Unloading filament...")
		return nil
	case "retry":
		if err := p.RetryFilamentAction(); err != nil {
			return fmt.Errorf("failed to retry filament action: %w", err)
		}
		fmt.Println("Retrying filament action...")
		return nil
	default:
		return fmt.Errorf("unknown filament action: %s (use load|unload|retry)", args[0])
	}
}

// cmdReboot handles the reboot command.
func cmdReboot(p *printer.Printer, args []string) error {
	fmt.Println("WARNING: This will reboot the printer!")
	if err := p.Reboot(); err != nil {
		return fmt.Errorf("failed to send reboot command: %w", err)
	}
	fmt.Println("Sending reboot command...")
	return nil
}

// cmdFirmware is deprecated and has been removed.
// Firmware update/downgrade is not supported on Bambu printers.
func cmdFirmware(p *printer.Printer, args []string) error {
	fmt.Println("Firmware management is not supported via this SDK.")
	fmt.Println("Use Bambu Studio or Bambu Handy for firmware updates.")
	return nil
}

// cmdInfo handles the info command.
func cmdInfo(p *printer.Printer, args []string) error {
	fmt.Println("=== Printer Information ===")
	fmt.Println()
	fmt.Printf("Nozzle Type:     %s\n", p.NozzleType())
	fmt.Printf("Nozzle Diameter: %.1f mm\n", p.NozzleDiameter())
	fmt.Printf("Serial:          %s\n", p.Serial)
	fmt.Printf("IP Address:      %s\n", p.IPAddress)
	fmt.Printf("AMS Units:       %d\n", len(p.AMSHub().AMSHub))

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
				// Try to get human-readable filament name
				filamentName := tray.TrayInfoIdx
				if fil, err := filament.FilamentByIndex(tray.TrayInfoIdx); err == nil {
					filamentName = fil.Name()
				}
				fmt.Printf("  Tray %d: %s (%s)\n", trayIdx, filamentName, tray.TrayColor)
			}
		}
		fmt.Println()
	}

	return nil
}

// cmdCamera handles the camera command.
func cmdCamera(p *printer.Printer, args []string) error {
	fs := flag.NewFlagSet("camera", flag.ExitOnError)
	outputFile := fs.String("o", "frame.jpg", "Output file path")
	count := fs.Int("n", 1, "Number of frames to capture (0 for continuous until Ctrl+C)")
	interval := fs.Duration("i", 1*time.Second, "Interval between frames (for multi-frame capture)")
	timeout := fs.Duration("timeout", 10*time.Second, "Timeout for waiting for each frame")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *count == 1 {
		// Single frame capture
		fmt.Printf("Capturing camera frame (timeout: %v)...\n", *timeout)

		frameBytes, err := p.CaptureFrameWithTimeout(*timeout)
		if err != nil {
			return fmt.Errorf("failed to capture frame: %w", err)
		}

		if err := os.WriteFile(*outputFile, frameBytes, 0600); err != nil {
			return fmt.Errorf("failed to save image: %w", err)
		}

		fmt.Printf("Camera frame saved to: %s (%d bytes)\n", *outputFile, len(frameBytes))
		return nil
	}

	// Multi-frame capture
	fmt.Printf("Capturing %d frames with %v interval (timeout: %v per frame)...\n", *count, *interval, *timeout)

	for i := 0; i < *count || *count == 0; i++ {
		fmt.Printf("Capturing frame %d... ", i+1)
		frameBytes, err := p.CaptureFrameWithTimeout(*timeout)
		if err != nil {
			return fmt.Errorf("failed to capture frame %d: %w", i+1, err)
		}

		filename := fmt.Sprintf("frame_%d.jpg", i+1)
		if *outputFile != "frame.jpg" {
			// Use output file as prefix if specified
			filename = fmt.Sprintf("%s_%d.jpg", strings.TrimSuffix(*outputFile, ".jpg"), i+1)
		}

		if err := os.WriteFile(filename, frameBytes, 0600); err != nil {
			return fmt.Errorf("failed to save frame %d: %w", i+1, err)
		}

		fmt.Printf("saved: %s (%d bytes)\n", filename, len(frameBytes))

		if *count > 0 && i < *count-1 {
			time.Sleep(*interval)
		} else if *count == 0 {
			time.Sleep(*interval)
		}
	}

	return nil
}

// cmdFTPList handles the ftp-list command.
func cmdFTPList(p *printer.Printer, args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}

	fmt.Printf("Listing directory: %s\n", path)
	files, err := p.ListDirectory(path)
	if err != nil {
		return fmt.Errorf("failed to list directory: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("Directory is empty")
		return nil
	}

	fmt.Printf("Found %d files/directories:\n", len(files))
	for _, file := range files {
		fmt.Printf("  %s\n", file)
	}
	return nil
}

// cmdPrint handles the print command (upload and print a 3MF/Gcode file).
func cmdPrint(p *printer.Printer, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bambu-cli print <file.3mf|file.gcode> [options]\nUse --help for options")
	}

	fs := flag.NewFlagSet("print", flag.ExitOnError)
	plate := fs.Int("plate", 1, "Plate number (1-4)")
	useAMS := fs.Bool("ams", true, "Use AMS filament system")
	amsMapping := fs.Int("ams-map", 0, "AMS slot mapping (0-7)")
	flowCalib := fs.Bool("flow-calib", true, "Enable flow calibration")
	bedType := fs.String("bed", "textured_plate", "Bed type (textured_plate, smooth_plate, etc.)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if len(fs.Args()) < 1 {
		return fmt.Errorf("missing file argument")
	}

	localPath := fs.Args()[0]

	fmt.Printf("Submitting print job: %s\n", localPath)
	fmt.Printf("  Plate: %d\n", *plate)
	fmt.Printf("  AMS: %v (slot %d)\n", *useAMS, *amsMapping)
	fmt.Printf("  Flow Calibration: %v\n", *flowCalib)
	fmt.Printf("  Bed Type: %s\n", *bedType)

	uploadedPath, err := p.SubmitPrintJobFromFile(localPath, *plate, *useAMS, []int{*amsMapping}, *flowCalib, *bedType)
	if err != nil {
		return fmt.Errorf("failed to submit print job: %w", err)
	}

	fmt.Printf("Print job submitted successfully!\n")
	fmt.Printf("  Uploaded file: %s\n", uploadedPath)
	return nil
}

// printUsage prints the main usage information.
func printUsage() {
	fmt.Print(`bambu-cli - Bambu Lab CLI tool

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
  firmware            Show firmware info (deprecated)
  reboot              Reboot the printer
  camera              Capture camera frame(s) (-o output, -n count, -i interval)
  print <file>        Upload and print a 3MF/Gcode file (--plate, --ams, --bed)
  ftp-list [path]     List files via FTP (default: root directory)

Camera Examples:
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 camera
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 camera -o myframe.jpg
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 camera -n 10 -i 2s

Print Examples:
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 print model.3mf
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 print model.3mf --plate 2 --bed smooth_plate

Other Examples:
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 temp --nozzle 220 --bed 60
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 light on
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 gcode "G28"
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status --json
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 status --watch
  bambu-cli -ip 192.168.1.100 -code 12345678 -serial ABC123 ftp-list image
`)
}

func run() int {
	cfg, command, cmdArgs := loadConfig()

	// Check if help is requested or no command provided
	if len(os.Args) < 2 {
		printUsage()
		return 0
	}

	// Handle help commands
	if command == "-h" || command == "--help" || command == "help" {
		printUsage()
		return 0
	}

	// Validate config for all commands except help
	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printUsage()
		return 1
	}

	// Connect to printer via MQTT only (no camera for commands)
	p, err := connectMQTT(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to printer: %v\n", err)
		return 1
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
	case "camera":
		cmdErr = cmdCamera(p, cmdArgs)
	case "print":
		cmdErr = cmdPrint(p, cmdArgs)
	case "ftp-list":
		cmdErr = cmdFTPList(p, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		return 1
	}

	if cmdErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", cmdErr)
		return 1
	}

	return 0
}

func main() {
	os.Exit(run())
}
