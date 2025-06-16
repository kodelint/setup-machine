package installer

import (
	"os"
	"os/exec"
	"path/filepath"
	"setup-machine/internal/config"
	"strings"
)

// uninstallTool attempts to remove a tool based on the information provided in toolState.
// It supports direct file removal, macOS pkgutil package forgetting, and glob-based matching.
func uninstallTool(name string, toolState config.ToolState) bool {
	config.Info("[INFO] Uninstalling %s...\n", name)

	installPath := toolState.InstallPath

	// Uninstall using Homebrew if path indicates Homebrew installation
	if strings.HasPrefix(installPath, "/opt/homebrew/bin/") {
		config.Info("[INFO] Detected Homebrew tool. Uninstalling with brew...\n")
		cmd := exec.Command("brew", "uninstall", name)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] brew uninstall output: %s\n", output)
		if err != nil {
			config.Error("[ERROR] Brew uninstall failed: %v\n", err)
			return false
		}
		return true
	}

	// Uninstall Go tools by removing binary
	if strings.HasPrefix(installPath, filepath.Join(os.Getenv("HOME"), "go/bin/")) {
		config.Info("[INFO] Detected Go tool. Removing binary directly...\n")
		if err := os.Remove(installPath); err == nil {
			config.Info("[INFO] Successfully removed Go binary %s\n", installPath)
			return true
		} else {
			config.Error("[ERROR] Failed to remove Go binary %s: %v\n", installPath, err)
			return false
		}
	}

	// Uninstall Rustup or Cargo tools
	if strings.HasPrefix(installPath, filepath.Join(os.Getenv("HOME"), ".cargo/bin/")) {
		config.Info("[INFO] Detected Rust tool. Determining uninstall strategy...\n")

		// Check if it's a rustup component (rustfmt, clippy, rust-analyzer, etc.)
		showCmd := exec.Command("rustup", "show", "active-toolchain")
		output, err := showCmd.CombinedOutput()
		activeToolchain := strings.TrimSpace(string(output))
		config.Debug("[DEBUG] rustup active-toolchain output: %s\n", activeToolchain)

		if err != nil {
			config.Error("[ERROR] Failed to query rustup active toolchain: %v\n", err)
		} else if strings.Contains(activeToolchain, "system") {
			config.Warn("[WARN] Cannot uninstall rustup component — toolchain is 'system'.\n")
			config.Warn("[WARN] To switch to a proper toolchain, run: rustup install stable && rustup default stable\n")
			return false
		} else {
			config.Info("[INFO] Cannot uninstall rustup component directly. Manual cleanup may be required.\n")
			// You can optionally attempt to remove the binary directly if it's not a managed component
			if err := os.Remove(installPath); err == nil {
				config.Info("[INFO] Removed binary %s as fallback\n", installPath)
				return true
			}
			return false
		}

		// Otherwise, try cargo uninstall (non-rustup component)
		cmd := exec.Command("cargo", "uninstall", name)
		cargoOutput, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] cargo uninstall output: %s\n", cargoOutput)
		if err != nil {
			config.Error("[ERROR] Cargo uninstall failed: %v\n", err)
			return false
		}
		return true
	}

	// Fallback to direct file or directory removal
	if installPath != "" {
		config.Debug("[DEBUG] Attempting to remove %s\n", installPath)
		if err := os.Remove(installPath); err == nil {
			config.Info("[INFO] Successfully removed binary %s\n", installPath)
			return true
		} else if err := os.RemoveAll(installPath); err == nil {
			config.Info("[INFO] Successfully removed directory %s\n", installPath)
			return true
		}
	}

	// Try uninstalling .pkg files via macOS pkgutil
	config.Info("[INFO] Trying to uninstall %s as macOS .pkg...\n", name)
	pkgUtilCmd := exec.Command("pkgutil", "--pkgs")
	output, err := pkgUtilCmd.CombinedOutput()
	if err != nil {
		config.Error("[ERROR] Failed to query pkgutil: %v\nOutput: %s\n", err, output)
	} else {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, name) {
				forgetCmd := exec.Command("sudo", "pkgutil", "--forget", line)
				config.Debug("[DEBUG] Running pkgutil forget: %s\n", strings.Join(forgetCmd.Args, " "))
				out, err := forgetCmd.CombinedOutput()
				if err == nil {
					config.Info("[INFO] pkgutil forget succeeded for %s\n", line)
					return true
				} else {
					config.Error("[ERROR] pkgutil forget failed: %v\nOutput: %s\n", err, out)
				}
			}
		}
	}

	// Fallback: use globbing to match and delete binaries
	commonPaths := "/usr/local/bin/" + name + "*"
	matches, err := filepath.Glob(commonPaths)
	config.Debug("[DEBUG] Globbing matches %v\n", matches)
	if err != nil {
		config.Error("[ERROR] Failed to glob %s: %v\n", commonPaths, err)
	}

	if !globbingMatches(matches) {
		config.Debug("[DEBUG] Globbing did not yield valid matches\n")
		config.Error("[ERROR] Invalid or empty glob pattern %s\n", commonPaths)
	} else {
		return true
	}

	return false
}

func uninstallFont(name string, fontState config.FontState) bool {
	removed := false
	for _, file := range fontState.Files {
		err := os.Remove(file)
		if err == nil {
			config.Info("[INFO] Removed font file: %s\n", file)
			removed = true
		} else {
			config.Error("[ERROR] Failed to remove font file %s: %v\n", file, err)
		}
	}
	if !removed {
		config.Warn("[WARN] No font files removed for %s\n", name)
	}
	return removed
}
