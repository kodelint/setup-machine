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

// SyncTools synchronizes the installed tools with the desired config and current state.
// It installs new tools, upgrades outdated tools, and removes tools no longer in the config.
func SyncTools(tools []config.Tool, st *config.State) {
	config.Debug("[DEBUG] Starting SyncTools with %d tools, current state has %d entries\n", len(tools), len(st.Tools))

	existing := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use concurrency for installation/upgrades
	for _, tool := range tools {
		existing[tool.Name] = true

		curToolState, ok := st.Tools[tool.Name]
		if !ok || curToolState.Version != tool.Version {
			wg.Add(1)
			go func(tool config.Tool, curToolState config.ToolState, exists bool) {
				defer wg.Done()
				config.Debug("[DEBUG] SyncTools: Installing/upgrading %s (current: %s, target: %s)\n",
					tool.Name, curToolState.Version, tool.Version)

				success, installPath := installTool(tool)
				if success {
					config.Info("[INFO] Installed %s@%s\n", tool.Name, tool.Version)

					// Protect state update with a mutex
					mu.Lock()
					st.Tools[tool.Name] = config.ToolState{
						Version:             tool.Version,
						InstallPath:         installPath,
						InstalledByDevSetup: true,
					}
					mu.Unlock()
				} else {
					config.Error("[ERROR] Failed to install %s@%s\n", tool.Name, tool.Version)
				}
			}(tool, curToolState, ok)
		} else {
			config.Debug("[DEBUG] SyncTools: %s version %s is already current.\n", tool.Name, tool.Version)
			config.Info("[INFO] %s version %s is current. Skipping.\n", tool.Name, tool.Version)
		}
	}

	wg.Wait() // Wait for all installs to complete

	// Sequential removal to avoid conflicts with state modifications
	for name, toolState := range st.Tools {
		if !existing[name] {
			config.Warn("[WARN] %s removed from config. Uninstalling...\n", name)
			if uninstallTool(name, toolState) {
				delete(st.Tools, name)
			} else {
				config.Warn("[WARN] Failed to uninstall %s completely. Manual cleanup may be required.\n", name)
			}
		}
	}

	config.Debug("[DEBUG] Finished SyncTools\n")
}

// SyncSettings applies macOS user defaults settings from the config,
// and updates the state file with applied settings to avoid redundant changes.
func SyncSettings(settings []config.Setting, st *config.State) {
	// Iterate over each desired setting from config
	for _, s := range settings {
		// Compose a unique key to identify each setting (domain:key)
		key := fmt.Sprintf("%s:%s", s.Domain, s.Key)

		// Log the setting being considered with its value and type
		config.Debug("[DEBUG] Considering setting %s = %s (%s)\n", key, s.Value, s.Type)

		// Check if this setting is already applied with the same value in the state file
		if prev, ok := st.Settings[key]; ok && prev.Value == s.Value {
			// If yes, skip re-applying the setting for efficiency
			config.Info("[INFO] Skipping already applied setting %s = %s\n", key, s.Value)
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
			// Log Error if the setting application failed along with command output
			config.Error("[ERROR] Failed to apply setting %s: %v\nOutput: %s\n", key, err, output)
			continue
		}

		// Log successful setting application
		config.Info("[INFO] Applied setting: %s = %s\n", key, s.Value)

		// Update the state file with this newly applied setting
		st.Settings[key] = config.SettingState{
			Domain: s.Domain,
			Key:    s.Key,
			Value:  s.Value,
		}
	}
}

// SyncAliases ensures shell aliases from the config are added to the user's shell rc file.
// It avoids duplicate entries by checking existing aliases first.
func SyncAliases(aliases config.Aliases) {
	// Get current user Info for home directory and rc file path
	usr, err := user.Current()
	if err != nil {
		config.Error("[ERROR] Failed to get current user: %v\n", err)
		return
	}

	// Determine which shell to use for aliasing; default to detected shell if empty
	shell := aliases.Shell
	if shell == "" {
		shell = detectShell()
	}
	config.Debug("[DEBUG] Using shell '%s' for aliases\n", shell)

	// Map supported shells to their rc file names
	shellrcMap := map[string]string{
		"zsh":  ".zshrc",
		"bash": ".bashrc",
	}
	shellrc, ok := shellrcMap[shell]
	if !ok {
		// If shell unknown, Warn and default to .zshrc
		config.Warn("[WARN] Unknown shell '%s', defaulting to '.zshrc'\n", shell)
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
		config.Error("[ERROR] Unable to open file %s for appending: %v\n", rcPath, err)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			config.Error("[ERROR] Unable to close file %s for appending: %v\n", rcPath, err)
		}
	}(file)

	// Write raw configs if provided
	for _, raw := range aliases.RawConfigs {
		lines := strings.Split(raw, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || existing[trimmed] {
				config.Debug("[DEBUG] Raw config already exists or is empty: %s\n", trimmed)
				continue
			}
			if _, err := file.WriteString(trimmed + "\n"); err != nil {
				config.Error("[ERROR] Failed to write raw config line: %s: %v\n", trimmed, err)
			} else {
				config.Info("[INFO] Added raw shell config: %s\n", trimmed)
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
			config.Debug("[DEBUG] Alias already exists: %s\n", aliasCmd)
			continue
		}

		// Write new alias line to rc file
		if _, err := file.WriteString(aliasCmd + "\n"); err != nil {
			// Log failure to write alias
			config.Error("[ERROR] Failed to write alias '%s': %v\n", aliasCmd, err)
		} else {
			// Log successful alias addition
			config.Info("[INFO] Added alias: %s\n", aliasCmd)
			existing[aliasCmd] = true
		}
	}
}

func SyncFonts(fonts []config.Font, st *config.State) {
	// Track fonts defined in the current config
	configuredFonts := map[string]struct{}{}

	for _, font := range fonts {
		// Currently only GitHub is supported
		if font.Source != "github" {
			config.Warn("[WARN] Unsupported font source for %s: %s\n", font.Name, font.Source)
			continue
		}

		// Construct download URL for GitHub release asset
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s.zip", font.Repo, font.Tag, font.Name)
		configuredFonts[font.Name] = struct{}{}

		// Skip install if font is already installed with the same URL
		if existing, ok := st.Fonts[font.Name]; ok && existing.URL == url {
			config.Info("[INFO] Skipping already installed font: %s\n", font.Name)
			continue
		}

		// Proceed with font installation
		files, err := installFont(font.Name, url)
		if err != nil {
			config.Error("[ERROR] Failed to install font %s: %v\n", font.Name, err)
			continue
		}

		if len(files) == 0 {
			config.Warn("[WARN] No Regular fonts installed for %s, skipping state update\n", font.Name)
			continue
		}

		// Save installed font info to state
		st.Fonts[font.Name] = config.FontState{
			Name:  font.Name,
			URL:   url,
			Files: files,
		}
		config.Info("[INFO] Installed font: %s\n", font.Name)
	}

	// Uninstall fonts from state that are not in the current config
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
