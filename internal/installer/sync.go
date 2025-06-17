package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"setup-machine/internal/config"
	"strings"
	"sync"
)

// SyncTools synchronizes the tools installed on the system with the desired
// tool configuration and the current state.
// It installs new tools, upgrades tools with version mismatch, and removes
// tools that no longer appear in the config.
func SyncTools(tools []config.Tool, st *config.State) {
	// Debug log: Starting SyncTools with counts of desired tools and current known state tools
	config.Debug("[DEBUG] Starting SyncTools with %d tools, current state has %d entries\n", len(tools), len(st.Tools))

	// Track which tools exist in the new config to compare for removal later
	existing := make(map[string]bool)

	// Mutex to protect concurrent writes to shared state struct
	var mu sync.Mutex
	// WaitGroup to wait for all concurrent installs/upgrades to complete before continuing
	var wg sync.WaitGroup

	// Iterate over all tools defined in the config
	for _, tool := range tools {
		// Mark tool as existing in the config
		existing[tool.Name] = true

		// Retrieve current tool state from the statefile (if any)
		curToolState, ok := st.Tools[tool.Name]

		// If tool is new (not in state) OR version mismatch => install or upgrade
		if !ok || curToolState.Version != tool.Version {
			wg.Add(1) // Add a goroutine to WaitGroup

			// Launch concurrent goroutine for installing/upgrading this tool
			go func(tool config.Tool, curToolState config.ToolState, exists bool) {
				defer wg.Done() // Signal WaitGroup on goroutine exit

				// Debug log: Inform which tool is being installed/upgraded, with versions
				config.Debug("[DEBUG] SyncTools: Installing/upgrading %s (current: %s, target: %s)\n",
					tool.Name, curToolState.Version, tool.Version)

				// Call installTool which returns success flag and installation path
				success, installPath := installTool(tool)
				if success {
					// Log success info
					config.Info("[INFO] Installed %s@%s\n", tool.Name, tool.Version)

					// Lock mutex before updating shared state
					mu.Lock()
					st.Tools[tool.Name] = config.ToolState{
						Version:             tool.Version,
						InstallPath:         installPath,
						InstalledByDevSetup: true, // Mark as installed by this tool
					}
					mu.Unlock() // Unlock mutex after update
				} else {
					// Log error on install failure
					config.Error("[ERROR] Failed to install %s@%s\n", tool.Name, tool.Version)
				}
			}(tool, curToolState, ok)
		} else {
			// Tool already installed with correct version, skip installation
			config.Debug("[DEBUG] SyncTools: %s version %s is already current.\n", tool.Name, tool.Version)
			config.Info("[INFO] %s version %s is current. Skipping.\n", tool.Name, tool.Version)
		}
	}

	// Wait for all install/upgrade goroutines to complete before proceeding
	wg.Wait()

	// After install/upgrade, remove any tools in state that are not in config anymore
	// We do this sequentially to avoid concurrent map modification issues
	for name, toolState := range st.Tools {
		// If tool name not in the current config, uninstall it
		if !existing[name] {
			config.Warn("[WARN] %s removed from config. Uninstalling...\n", name)

			// Attempt uninstall; if successful, delete from state; else log warning
			if uninstallTool(name, toolState) {
				delete(st.Tools, name)
			} else {
				config.Warn("[WARN] Failed to uninstall %s completely. Manual cleanup may be required.\n", name)
			}
		}
	}

	// Debug log marking completion of SyncTools
	config.Debug("[DEBUG] Finished SyncTools\n")
}

// SyncSettings applies macOS user defaults settings from the config,
// and updates the state file with applied settings to avoid redundant changes on next runs.
func SyncSettings(settings []config.Setting, st *config.State) {
	// Iterate over each desired macOS setting in the config
	for _, s := range settings {
		// Compose a unique key to identify setting in the form domain:key
		key := fmt.Sprintf("%s:%s", s.Domain, s.Key)

		// Debug log about setting being considered with its type and value
		config.Debug("[DEBUG] Considering setting %s = %s (%s)\n", key, s.Value, s.Type)

		// Check if setting is already applied (exists in state with same value)
		if prev, ok := st.Settings[key]; ok && prev.Value == s.Value {
			// Skip applying the setting again to save time
			config.Info("[INFO] Skipping already applied setting %s = %s\n", key, s.Value)
			continue
		}

		// Build arguments for the 'defaults write' command according to setting type
		args := []string{"write", s.Domain, s.Key}
		switch s.Type {
		case "bool":
			args = append(args, "-bool", s.Value)
		case "int":
			args = append(args, "-int", s.Value)
		case "float":
			args = append(args, "-float", s.Value)
		default:
			// Default to string type if type is unknown
			args = append(args, "-string", s.Value)
		}

		// Execute the 'defaults' command with constructed args to apply setting
		cmd := exec.Command("defaults", args...)
		output, err := cmd.CombinedOutput() // Capture output and error

		if err != nil {
			// Log error along with command output on failure
			config.Error("[ERROR] Failed to apply setting %s: %v\nOutput: %s\n", key, err, output)
			continue
		}

		// Log success message after applying setting
		config.Info("[INFO] Applied setting: %s = %s\n", key, s.Value)

		// Update the state file to reflect the applied setting and avoid re-applying next time
		st.Settings[key] = config.SettingState{
			Domain: s.Domain,
			Key:    s.Key,
			Value:  s.Value,
		}
	}
}

// SyncAliases ensures shell aliases from the config are appended to the user's
// shell RC file, avoiding duplicates by checking existing aliases first.
func SyncAliases(aliases config.Aliases) {
	// Retrieve current user info (mainly for home directory path)
	usr, err := user.Current()
	if err != nil {
		config.Error("[ERROR] Failed to get current user: %v\n", err)
		return
	}

	// Determine which shell to target; default to detected shell if empty
	shell := aliases.Shell
	if shell == "" {
		shell = detectShell()
	}
	config.Debug("[DEBUG] Using shell '%s' for aliases\n", shell)

	// Map of common shell names to their rc filenames
	shellrcMap := map[string]string{
		"zsh":  ".zshrc",
		"bash": ".bashrc",
	}
	// Determine rc file name for shell, fallback to .zshrc if unknown
	shellrc, ok := shellrcMap[shell]
	if !ok {
		config.Warn("[WARN] Unknown shell '%s', defaulting to '.zshrc'\n", shell)
		shellrc = ".zshrc"
	}
	// Full path to the rc file
	rcPath := filepath.Join(usr.HomeDir, shellrc)

	// Read existing lines from rc file into a map to avoid duplicate alias insertion
	existing := make(map[string]bool)
	if f, err := os.Open(rcPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			existing[line] = true
		}
		_ = f.Close()
	}

	// Open rc file for appending new aliases
	file, err := os.OpenFile(rcPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		config.Error("[ERROR] Unable to open file %s for appending: %v\n", rcPath, err)
		return
	}
	// Ensure file gets closed properly after function returns
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			config.Error("[ERROR] Unable to close file %s for appending: %v\n", rcPath, err)
		}
	}(file)

	// Write raw config lines (if any) line-by-line after trimming
	for _, raw := range aliases.RawConfigs {
		// Some raw configs may have multiple lines separated by newlines
		lines := strings.Split(raw, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || existing[trimmed] {
				// Skip empty or duplicate lines
				config.Debug("[DEBUG] Raw config already exists or is empty: %s\n", trimmed)
				continue
			}
			// Write line to rc file
			if _, err := file.WriteString(trimmed + "\n"); err != nil {
				config.Error("[ERROR] Failed to write raw config line: %s: %v\n", trimmed, err)
			} else {
				config.Info("[INFO] Added raw shell config: %s\n", trimmed)
				existing[trimmed] = true
			}
		}
	}

	// Iterate over all alias entries from config and add them if missing
	for _, a := range aliases.Entries {
		// Format alias string e.g. alias gs="git status"
		aliasCmd := fmt.Sprintf("alias %s=\"%s\"", a.Name, a.Value)

		// Skip alias if it already exists in the rc file
		if existing[aliasCmd] {
			config.Debug("[DEBUG] Alias already exists: %s\n", aliasCmd)
			continue
		}

		// Write the alias command line to the rc file
		if _, err := file.WriteString(aliasCmd + "\n"); err != nil {
			config.Error("[ERROR] Failed to write alias '%s': %v\n", aliasCmd, err)
		} else {
			config.Info("[INFO] Added alias: %s\n", aliasCmd)
			existing[aliasCmd] = true
		}
	}
}

// SyncFonts installs, updates, and uninstalls fonts as per the config and state.
// It supports fonts sourced from GitHub releases currently.
func SyncFonts(fonts []config.Font, st *config.State) {
	// Track fonts defined in the current config for later removal of stale fonts
	configuredFonts := map[string]struct{}{}

	// Iterate over all fonts declared in the config
	for _, font := range fonts {
		// Support only GitHub source currently; log warning for others
		if font.Source != "github" {
			config.Warn("[WARN] Unsupported font source for %s: %s\n", font.Name, font.Source)
			continue
		}

		// Construct the URL for the font zip archive from GitHub releases
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s.zip", font.Repo, font.Tag, font.Name)

		// Mark font as configured for tracking
		configuredFonts[font.Name] = struct{}{}

		// Skip installation if font already installed at this URL (no changes)
		if existing, ok := st.Fonts[font.Name]; ok && existing.URL == url {
			config.Info("[INFO] Skipping already installed font: %s\n", font.Name)
			continue
		}

		// Proceed to install the font by downloading and extracting
		files, err := installFont(font.Name, url)
		if err != nil {
			config.Error("[ERROR] Failed to install font %s: %v\n", font.Name, err)
			continue
		}

		// Warn if no 'Regular' font files found and skip state update
		if len(files) == 0 {
			config.Warn("[WARN] No Regular fonts installed for %s, skipping state update\n", font.Name)
			continue
		}

		// Update state with newly installed font info (name, URL, files)
		st.Fonts[font.Name] = config.FontState{
			Name:  font.Name,
			URL:   url,
			Files: files,
		}
		config.Info("[INFO] Installed font: %s\n", font.Name)
	}

	// Uninstall fonts no longer present in the config by comparing to state
	for name, fontState := range st.Fonts {
		if _, found := configuredFonts[name]; !found {
			config.Info("[INFO] Font %s no longer in config. Uninstalling...\n", name)
			if uninstallFont(name, fontState) {
				delete(st.Fonts, name)
				config.Info("[INFO] Successfully uninstalled font: %s\n", name)
			} else {
				config.Warn("[WARN] Failed to fully uninstall font: %s\n", name)
			}
		}
	}
}
