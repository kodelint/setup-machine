package config

import "github.com/fatih/color"

// Define colorized printing functions for different log levels using fatih/color.
// These are package-level variables holding functions that behave like fmt.Printf,
// but with text colored appropriately for the log level.

// Info logs informational messages in green color.
// Green is typically used for success or normal Info to catch user attention pleasantly.
var Info = color.New(color.FgGreen).PrintfFunc()

// Warn logs warning messages in bright magenta color.
// Magenta is bright and stands out, signaling caution without being too alarming.
var Warn = color.New(color.FgHiMagenta).PrintfFunc()

// Error logs Error messages in red color.
// Red is commonly associated with Error or critical problems to draw immediate attention.
var Error = color.New(color.FgRed).PrintfFunc()

// Debug logs Debug messages in cyan color if enabled, otherwise is a no-op.
// This is a function variable that is assigned dynamically during Init based on Debug flag.
// When Debug logging is disabled, Debug is assigned to an empty function that does nothing.
var Debug func(format string, a ...any)

// Init initializes the logger package, specifically enabling or disabling Debug logging.
// Parameters:
// - enableDebug: boolean flag to turn Debug messages on or off.
// When enabled, Debug will print messages in cyan color.
// When disabled, Debug will be a no-op function that silently ignores Debug logs.
func Init(enableDebug bool) {
	if enableDebug {
		// Assign Debug to print cyan-colored Debug messages.
		Debug = color.New(color.FgCyan).PrintfFunc()
	} else {
		// Assign Debug to a no-op function that ignores all Debug logs to avoid runtime overhead.
		Debug = func(format string, a ...any) {}
	}
}
