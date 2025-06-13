package installer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"setup-machine/internal/config"
	"setup-machine/internal/logger"
	"setup-machine/internal/state"
	"strings"
)

// SyncTools synchronizes the installed tools with the desired config and current state.
// It installs new tools, upgrades outdated tools, and removes tools no longer in the config.
func SyncTools(tools []config.Tool, st *state.State) {
	// Log starting info: how many tools to process and current state entries
	logger.Debug("[DEBUG] Starting SyncTools with %d tools, current state has %d entries\n", len(tools), len(st.Tools))

	// Track tools that are present in the current config
	existing := map[string]bool{}

	// Iterate over all desired tools from the config
	for _, tool := range tools {
		existing[tool.Name] = true // Mark this tool as existing in config

		// Get current state of this tool from the saved state file
		curToolState, ok := st.Tools[tool.Name]

		// Check if the tool is missing or the version differs from desired
		if !ok || curToolState.Version != tool.Version {
			logger.Debug("[DEBUG] SyncTools: Installing/upgrading %s (current: %s, target: %s)\n", tool.Name, curToolState.Version, tool.Version)

			// Attempt to install or upgrade the tool
			success, installPath := installTool(tool)
			if success {
				// Log success and update the state with the new version and install path
				logger.Info("[INFO] Installed %s@%s\n", tool.Name, tool.Version)
				st.Tools[tool.Name] = state.ToolState{
					Version:             tool.Version,
					InstallPath:         installPath,
					InstalledByDevSetup: true,
				}
			} else {
				// Log failure to install
				logger.Error("[ERROR] Failed to install %s@%s\n", tool.Name, tool.Version)
			}
		} else {
			// Tool is already at the desired version; no action needed
			logger.Debug("[DEBUG] SyncTools: %s version %s is already current.\n", tool.Name, tool.Version)
			logger.Info("[INFO] %s version %s is current. Skipping.\n", tool.Name, tool.Version)
		}
	}

	// Now handle tools that exist in the state but are no longer in the config (should be removed)
	for name, toolState := range st.Tools {
		if !existing[name] {
			// Tool was removed from config; uninstall it
			logger.Warn("[WARN] %s removed from config. Uninstalling...\n", name)
			if uninstallTool(name, toolState) {
				delete(st.Tools, name)
			} else {
				logger.Warn("[WARN] Failed to uninstall %s completely. Manual cleanup may be required.\n", name)
			}

		}
	}
	logger.Debug("[DEBUG] Finished SyncTools\n")
}

// SyncSettings applies macOS user defaults settings from the config,
// and updates the state file with applied settings to avoid redundant changes.
func SyncSettings(settings []config.Setting, st *state.State) {
	// Iterate over each desired setting from config
	for _, s := range settings {
		// Compose a unique key to identify each setting (domain:key)
		key := fmt.Sprintf("%s:%s", s.Domain, s.Key)

		// Log the setting being considered with its value and type
		logger.Debug("[DEBUG] Considering setting %s = %s (%s)\n", key, s.Value, s.Type)

		// Check if this setting is already applied with the same value in the state file
		if prev, ok := st.Settings[key]; ok && prev.Value == s.Value {
			// If yes, skip re-applying the setting for efficiency
			logger.Info("[INFO] Skipping already applied setting %s = %s\n", key, s.Value)
			continue
		}

		// Build the arguments for the `defaults write` command based on setting type
		args := []string{"write", s.Domain, s.Key}
		switch s.Type {
		case "bool":
			args = append(args, "-bool", s.Value)
		case "int":
			args = append(args, "-int", s.Value)
		case "float":
			args = append(args, "-float", s.Value)
		default:
			// Default to string type if none of the above
			args = append(args, "-string", s.Value)
		}

		// Execute the defaults command with constructed arguments
		cmd := exec.Command("defaults", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Log error if the setting application failed along with command output
			logger.Error("[ERROR] Failed to apply setting %s: %v\nOutput: %s\n", key, err, output)
			continue
		}

		// Log successful setting application
		logger.Info("[INFO] Applied setting: %s = %s\n", key, s.Value)

		// Update the state file with this newly applied setting
		st.Settings[key] = state.SettingState{
			Domain: s.Domain,
			Key:    s.Key,
			Value:  s.Value,
		}
	}
}

// SyncAliases ensures shell aliases from the config are added to the user's shell rc file.
// It avoids duplicate entries by checking existing aliases first.
func SyncAliases(aliases config.Aliases) {
	// Get current user info for home directory and rc file path
	usr, err := user.Current()
	if err != nil {
		logger.Error("[ERROR] Failed to get current user: %v\n", err)
		return
	}

	// Determine which shell to use for aliasing; default to detected shell if empty
	shell := aliases.Shell
	if shell == "" {
		shell = detectShell()
	}
	logger.Debug("[DEBUG] Using shell '%s' for aliases\n", shell)

	// Map supported shells to their rc file names
	shellrcMap := map[string]string{
		"zsh":  ".zshrc",
		"bash": ".bashrc",
	}
	shellrc, ok := shellrcMap[shell]
	if !ok {
		// If shell unknown, warn and default to .zshrc
		logger.Warn("[WARN] Unknown shell '%s', defaulting to '.zshrc'\n", shell)
		shellrc = ".zshrc"
	}
	// Construct full path to shell rc file
	rcPath := filepath.Join(usr.HomeDir, shellrc)

	// Read existing lines from the rc file to avoid duplicates
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
		logger.Error("[ERROR] Unable to open file %s for appending: %v\n", rcPath, err)
		return
	}
	defer file.Close()

	// Write raw configs if provided
	for _, raw := range aliases.RawConfigs {
		lines := strings.Split(raw, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || existing[trimmed] {
				logger.Debug("[DEBUG] Raw config already exists or is empty: %s\n", trimmed)
				continue
			}
			if _, err := file.WriteString(trimmed + "\n"); err != nil {
				logger.Error("[ERROR] Failed to write raw config line: %s: %v\n", trimmed, err)
			} else {
				logger.Info("[INFO] Added raw shell config: %s\n", trimmed)
				existing[trimmed] = true
			}
		}
	}

	// Iterate over all aliases defined in config
	for _, a := range aliases.Entries {
		// Format alias command string e.g. alias gs="git status"
		aliasCmd := fmt.Sprintf("alias %s=\"%s\"", a.Name, a.Value)

		// Skip if alias already exists in rc file
		if existing[aliasCmd] {
			logger.Debug("[DEBUG] Alias already exists: %s\n", aliasCmd)
			continue
		}

		// Write new alias line to rc file
		if _, err := file.WriteString(aliasCmd + "\n"); err != nil {
			// Log failure to write alias
			logger.Error("[ERROR] Failed to write alias '%s': %v\n", aliasCmd, err)
		} else {
			// Log successful alias addition
			logger.Info("[INFO] Added alias: %s\n", aliasCmd)
			existing[aliasCmd] = true
		}
	}
}

// detectShell attempts to identify the current user's shell by inspecting the SHELL env variable.
// Returns "zsh" or "bash" or defaults to "zsh" if unknown.
func detectShell() string {
	shell := os.Getenv("SHELL")
	logger.Debug("[DEBUG] Detected shell environment: %s\n", shell)

	// Match common shell strings to either zsh or bash
	if strings.Contains(shell, "zsh") {
		return "zsh"
	} else if strings.Contains(shell, "bash") {
		return "bash"
	}
	// Default fallback
	return "zsh"
}

// uninstallTool attempts to remove a tool based on the information provided in toolState.
// It supports direct file removal, macOS pkgutil package forgetting, and glob-based matching.
func uninstallTool(name string, toolState state.ToolState) bool {
	logger.Info("[INFO] Uninstalling %s...\n", name)

	// First, attempt to remove the tool using the exact install path from state
	if toolState.InstallPath != "" {
		logger.Debug("[DEBUG] Attempting to remove %s\n", toolState.InstallPath)

		// Try removing the file at the install path
		if err := os.Remove(toolState.InstallPath); err == nil {
			logger.Info("[INFO] Successfully removed binary %s\n", toolState.InstallPath)
			return true
		}

		// If removal failed, try removing as a directory (useful for tools installed as folders)
		if err := os.RemoveAll(toolState.InstallPath); err == nil {
			logger.Info("[INFO] Successfully removed directory %s\n", toolState.InstallPath)
			return true
		}
	}

	// Attempt to uninstall the tool via macOS pkgutil
	logger.Info("[INFO] Trying to uninstall %s as macOS .pkg...\n", name)
	pkgutilCmd := exec.Command("pkgutil", "--pkgs")
	output, err := pkgutilCmd.CombinedOutput()
	if err != nil {
		logger.Error("[ERROR] Failed to query pkgutil: %v\nOutput: %s\n", err, output)
	} else {
		// Iterate through the list of installed packages
		for _, line := range strings.Split(string(output), "\n") {
			// If the package name contains our tool name
			if strings.Contains(line, name) {
				forgetCmd := exec.Command("sudo", "pkgutil", "--forget", line)
				logger.Debug("[DEBUG] Running pkgutil forget: %s\n", strings.Join(forgetCmd.Args, " "))
				out, err := forgetCmd.CombinedOutput()
				if err == nil {
					logger.Info("[INFO] pkgutil forget succeeded for %s\n", line)
					return true
				} else {
					logger.Error("[ERROR] pkgutil forget failed: %v\nOutput: %s\n", err, out)
				}
			}
		}
	}

	// Fallback: use globbing to match common install paths
	commonPaths := "/usr/local/bin/" + name + "*"
	matches, err := filepath.Glob(commonPaths)
	logger.Debug("[DEBUG] Globbing matches %v\n", matches)
	if err != nil {
		logger.Error("[ERROR] Failed to glob %s: %v\n", commonPaths, err)
	}

	// If any glob matches exist, try to remove them
	if !globbingMatches(matches) {
		logger.Debug("[DEBUG] Globbing did not yield valid matches\n")
		logger.Error("[ERROR] Invalid or empty glob pattern %s\n", commonPaths)
	} else {
		return true
	}

	// If all uninstall attempts failed, return false
	return false
}

// globbingMatches executes sudo rm on each glob match to remove the binary.
// Returns true if any files were successfully removed.
func globbingMatches(matches []string) bool {
	result := false
	for _, match := range matches {
		logger.Info("[INFO] Removing matched binary: %s\n", match)

		// Run sudo rm -f on the match
		cmd := exec.Command("sudo", "rm", "-f", match)
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("[ERROR] Failed to remove %s: %v\nOutput: %s\n", match, err, output)
		} else {
			logger.Info("[INFO] Successfully removed %s\n", match)
			result = true
		}
	}
	return result
}
