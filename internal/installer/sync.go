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
	"sync"
)

// SyncTools synchronizes the installed tools with the desired config and current state.
// It installs new tools, upgrades outdated tools, and removes tools no longer in the config.
func SyncTools(tools []config.Tool, st *state.State) {
	logger.Debug("[DEBUG] Starting SyncTools with %d tools, current state has %d entries\n", len(tools), len(st.Tools))

	existing := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use concurrency for installation/upgrades
	for _, tool := range tools {
		existing[tool.Name] = true

		curToolState, ok := st.Tools[tool.Name]
		if !ok || curToolState.Version != tool.Version {
			wg.Add(1)
			go func(tool config.Tool, curToolState state.ToolState, exists bool) {
				defer wg.Done()
				logger.Debug("[DEBUG] SyncTools: Installing/upgrading %s (current: %s, target: %s)\n",
					tool.Name, curToolState.Version, tool.Version)

				success, installPath := installTool(tool)
				if success {
					logger.Info("[INFO] Installed %s@%s\n", tool.Name, tool.Version)

					// Protect state update with a mutex
					mu.Lock()
					st.Tools[tool.Name] = state.ToolState{
						Version:             tool.Version,
						InstallPath:         installPath,
						InstalledByDevSetup: true,
					}
					mu.Unlock()
				} else {
					logger.Error("[ERROR] Failed to install %s@%s\n", tool.Name, tool.Version)
				}
			}(tool, curToolState, ok)
		} else {
			logger.Debug("[DEBUG] SyncTools: %s version %s is already current.\n", tool.Name, tool.Version)
			logger.Info("[INFO] %s version %s is current. Skipping.\n", tool.Name, tool.Version)
		}
	}

	wg.Wait() // Wait for all installs to complete

	// Sequential removal to avoid conflicts with state modifications
	for name, toolState := range st.Tools {
		if !existing[name] {
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

	installPath := toolState.InstallPath

	// Uninstall using Homebrew if path indicates Homebrew installation
	if strings.HasPrefix(installPath, "/opt/homebrew/bin/") {
		logger.Info("[INFO] Detected Homebrew tool. Uninstalling with brew...\n")
		cmd := exec.Command("brew", "uninstall", name)
		output, err := cmd.CombinedOutput()
		logger.Debug("[DEBUG] brew uninstall output: %s\n", output)
		if err != nil {
			logger.Error("[ERROR] Brew uninstall failed: %v\n", err)
			return false
		}
		return true
	}

	// Uninstall Go tools by removing binary
	if strings.HasPrefix(installPath, filepath.Join(os.Getenv("HOME"), "go/bin/")) {
		logger.Info("[INFO] Detected Go tool. Removing binary directly...\n")
		if err := os.Remove(installPath); err == nil {
			logger.Info("[INFO] Successfully removed Go binary %s\n", installPath)
			return true
		} else {
			logger.Error("[ERROR] Failed to remove Go binary %s: %v\n", installPath, err)
			return false
		}
	}

	// Uninstall Rustup or Cargo tools
	if strings.HasPrefix(installPath, filepath.Join(os.Getenv("HOME"), ".cargo/bin/")) {
		logger.Info("[INFO] Detected Rust tool. Determining uninstall strategy...\n")

		// Check if it's a rustup component (rustfmt, clippy, rust-analyzer, etc.)
		showCmd := exec.Command("rustup", "show", "active-toolchain")
		output, err := showCmd.CombinedOutput()
		activeToolchain := strings.TrimSpace(string(output))
		logger.Debug("[DEBUG] rustup active-toolchain output: %s\n", activeToolchain)

		if err != nil {
			logger.Error("[ERROR] Failed to query rustup active toolchain: %v\n", err)
		} else if strings.Contains(activeToolchain, "system") {
			logger.Warn("[WARN] Cannot uninstall rustup component — toolchain is 'system'.\n")
			logger.Warn("[WARN] To switch to a proper toolchain, run: rustup install stable && rustup default stable\n")
			return false
		} else {
			logger.Info("[INFO] Cannot uninstall rustup component directly. Manual cleanup may be required.\n")
			// You can optionally attempt to remove the binary directly if it's not a managed component
			if err := os.Remove(installPath); err == nil {
				logger.Info("[INFO] Removed binary %s as fallback\n", installPath)
				return true
			}
			return false
		}

		// Otherwise, try cargo uninstall (non-rustup component)
		cmd := exec.Command("cargo", "uninstall", name)
		cargoOutput, err := cmd.CombinedOutput()
		logger.Debug("[DEBUG] cargo uninstall output: %s\n", cargoOutput)
		if err != nil {
			logger.Error("[ERROR] Cargo uninstall failed: %v\n", err)
			return false
		}
		return true
	}

	// Fallback to direct file or directory removal
	if installPath != "" {
		logger.Debug("[DEBUG] Attempting to remove %s\n", installPath)
		if err := os.Remove(installPath); err == nil {
			logger.Info("[INFO] Successfully removed binary %s\n", installPath)
			return true
		} else if err := os.RemoveAll(installPath); err == nil {
			logger.Info("[INFO] Successfully removed directory %s\n", installPath)
			return true
		}
	}

	// Try uninstalling .pkg files via macOS pkgutil
	logger.Info("[INFO] Trying to uninstall %s as macOS .pkg...\n", name)
	pkgutilCmd := exec.Command("pkgutil", "--pkgs")
	output, err := pkgutilCmd.CombinedOutput()
	if err != nil {
		logger.Error("[ERROR] Failed to query pkgutil: %v\nOutput: %s\n", err, output)
	} else {
		for _, line := range strings.Split(string(output), "\n") {
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

	// Fallback: use globbing to match and delete binaries
	commonPaths := "/usr/local/bin/" + name + "*"
	matches, err := filepath.Glob(commonPaths)
	logger.Debug("[DEBUG] Globbing matches %v\n", matches)
	if err != nil {
		logger.Error("[ERROR] Failed to glob %s: %v\n", commonPaths, err)
	}

	if !globbingMatches(matches) {
		logger.Debug("[DEBUG] Globbing did not yield valid matches\n")
		logger.Error("[ERROR] Invalid or empty glob pattern %s\n", commonPaths)
	} else {
		return true
	}

	return false
}

// globbingMatches executes sudo rm on each glob match to remove the binary.
// Returns true if any files were successfully removed.
func globbingMatches(matches []string) bool {
	result := false
	for _, match := range matches {
		logger.Info("[INFO] Removing matched binary: %s\n", match)
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
