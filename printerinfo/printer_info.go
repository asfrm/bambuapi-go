// Package printerinfo provides printer information and types.
package printerinfo

// NozzleType represents the type of nozzle.
type NozzleType string

const (
	NozzleTypeStainlessSteel NozzleType = "stainless_steel"
	NozzleTypeHardenedSteel  NozzleType = "hardened_steel"
)

func (n NozzleType) String() string {
	return string(n)
}

// ParseNozzleType parses a string into a NozzleType.
func ParseNozzleType(s string) NozzleType {
	switch s {
	case "stainless_steel":
		return NozzleTypeStainlessSteel
	case "hardened_steel":
		return NozzleTypeHardenedSteel
	default:
		return NozzleTypeStainlessSteel
	}
}

// PrinterType represents the type of printer.
type PrinterType string

const (
	PrinterTypeUnknown PrinterType = "UNKNOWN"
	PrinterTypeP1S     PrinterType = "P1S"
	PrinterTypeP1P     PrinterType = "P1P"
	PrinterTypeA1      PrinterType = "A1"
	PrinterTypeA1Mini  PrinterType = "A1_MINI"
	PrinterTypeX1C     PrinterType = "X1C"
	PrinterTypeX1E     PrinterType = "X1E"
)

func (p PrinterType) String() string {
	return string(p)
}

// ParsePrinterType parses a string into a PrinterType.
func ParsePrinterType(s string) PrinterType {
	switch s {
	case "P1S":
		return PrinterTypeP1S
	case "P1P":
		return PrinterTypeP1P
	case "A1":
		return PrinterTypeA1
	case "A1_MINI":
		return PrinterTypeA1Mini
	case "X1C":
		return PrinterTypeX1C
	case "X1E":
		return PrinterTypeX1E
	default:
		return PrinterTypeUnknown
	}
}

// NozzleDiameters contains valid nozzle diameters.
var NozzleDiameters = map[float64]bool{
	0.2: true,
	0.4: true,
	0.6: true,
	0.8: true,
}

// IsValidNozzleDiameter checks if a nozzle diameter is valid.
func IsValidNozzleDiameter(d float64) bool {
	return NozzleDiameters[d]
}
