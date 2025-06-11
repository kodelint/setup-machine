package state

import (
	"encoding/json"                 // For JSON encoding and decoding of the state file
	"os"                            // For file system operations like reading and writing files
	"setup-machine/internal/logger" // Custom logger package for logging errors and debug info
)

// ToolState represents the saved state of an installed tool.
// It records the installed version, the full install path of the tool executable,
// and a boolean indicating whether this tool was installed by this setup system.
type ToolState struct {
	Version             string `json:"version"`                // Version string of the installed tool
	InstallPath         string `json:"install_path"`           // Absolute file system path where the tool executable is installed
	InstalledByDevSetup bool   `json:"installed_by_dev_setup"` // True if installed/managed by this setup tool, false if external/manual install
}

// SettingState represents the saved state of a macOS system setting that was applied.
// It stores the domain and key for the `defaults` system, plus the string value last applied.
type SettingState struct {
	Domain string `json:"domain"` // The domain string, e.g., "com.apple.finder"
	Key    string `json:"key"`    // The key string within that domain, e.g., "AppleShowAllFiles"
	Value  string `json:"value"`  // The value last written to that key, stored as string
}

// State holds the entire saved state for the setup tool.
// It includes maps of installed tools and applied system settings keyed by their unique identifiers.
type State struct {
	Tools    map[string]ToolState    `json:"tools"`    // Map from tool name to its ToolState
	Settings map[string]SettingState `json:"settings"` // Map from "domain:key" string to SettingState
}

// LoadState loads the saved state from a JSON file at the given path.
// If the file does not exist or cannot be read, it returns a new empty State struct.
// It ensures the Tools and Settings maps are non-nil to prevent nil pointer issues.
func LoadState(path string) *State {
	// Read entire state JSON file into memory
	file, err := os.ReadFile(path)
	if err != nil {
		// If file read fails (file missing, permission issues), return empty initialized state
		return &State{
			Tools:    make(map[string]ToolState),
			Settings: make(map[string]SettingState),
		}
	}

	// Parse JSON data into a State struct
	var st State
	_ = json.Unmarshal(file, &st)

	// Defensive: Ensure maps are initialized if JSON contained null for these fields
	if st.Tools == nil {
		st.Tools = make(map[string]ToolState)
	}
	if st.Settings == nil {
		st.Settings = make(map[string]SettingState)
	}

	return &st
}

// SaveState writes the given State struct to a JSON file at the given path.
// It pretty-prints the JSON with indentation for readability.
// Errors during marshalling or writing are logged but not propagated.
func SaveState(path string, st *State) {
	// Marshal the State struct into indented JSON bytes
	file, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		// Log marshalling errors, typically should never happen unless invalid data
		logger.Error("[ERROR] Failed to marshal state: %v\n", err)
		return
	}

	// Log debug info showing the full JSON state being written (can be verbose)
	logger.Debug("[DEBUG] Writing state to %s:\n%s\n", path, string(file))

	// Write the JSON bytes to the file with mode 0644 (read/write owner, read others)
	err = os.WriteFile(path, file, 0644)
	if err != nil {
		// Log write errors, e.g., permission denied or disk full
		logger.Error("[ERROR] Failed to write state file %s: %v\n", path, err)
	}
}
