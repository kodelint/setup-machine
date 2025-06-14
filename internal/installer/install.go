package installer

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"setup-machine/internal/config"
	"setup-machine/internal/logger"
	"strings"
)

func installTool(tool config.Tool) (bool, string) {
	logger.Debug("[DEBUG] installTool: Installing tool %s from source %s\n", tool.Name, tool.Source)

	var installPath string
	var err error

	switch tool.Source {
	case "github":
		logger.Info("[INFO] Installing %s@%s from GitHub...\n", tool.Name, tool.Version)
		installPath, err = downloadFromGitHub(tool)
		if err != nil {
			logger.Error("[ERROR] Failed to install %s from GitHub: %v\n", tool.Name, err)
			return false, ""
		}

	case "url":
		logger.Info("[INFO] Installing %s from custom URL...\n", tool.Name)
		tmp := "/tmp/" + path.Base(tool.URL)

		// Download the file using curl
		curlCmd := exec.Command("curl", "-L", tool.URL, "-o", tmp)
		logger.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
		output, err := curlCmd.CombinedOutput()
		if err != nil {
			logger.Error("[ERROR] Download failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
			return false, ""
		}

		// If it's a .pkg file, install it using the macOS installer
		if strings.HasSuffix(tool.URL, ".pkg") {
			logger.Info("[INFO] Detected .pkg file for %s. Installing via macOS installer...\n", tool.Name)
			installCmd := exec.Command("sudo", "installer", "-pkg", tmp, "-target", "/")
			logger.Debug("[DEBUG] Running command: %s\n", strings.Join(installCmd.Args, " "))
			output, err = installCmd.CombinedOutput()
			if err != nil {
				logger.Error("[ERROR] .pkg installation failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}
			return true, "/Applications" // general location for GUI apps (may vary by .pkg)

		} else {
			// Otherwise, treat as archive
			asset, err := ExtractAndInstall(tmp, "/tmp/")
			if err != nil {
				return false, ""
			}
			logger.Debug("[DEBUG] Extracted asset to %s\n", asset)

			chmodCmd := exec.Command("chmod", "+x", asset)
			logger.Debug("[DEBUG] Running command: %s\n", strings.Join(chmodCmd.Args, " "))
			output, err = chmodCmd.CombinedOutput()
			if err != nil {
				logger.Error("[ERROR] chmod failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}
			installPath = asset
		}
	case "brew":
		logger.Info("[INFO] Installing %s using Homebrew...\n", tool.Name)
		cmd := exec.Command("arch", "-arm64", "brew", "install", tool.Name)
		output, err := cmd.CombinedOutput()
		logger.Debug("[DEBUG] brew install output: %s\n", output)
		if err != nil {
			logger.Error("[ERROR] Brew install failed: %v\n", err)
			return false, ""
		}
		return true, "/opt/homebrew/bin/" + tool.Name // assumes Homebrew path

	case "go":
		logger.Info("[INFO] Installing %s using go install...\n", tool.Name)
		gobin := filepath.Join(os.Getenv("HOME"), "go", "bin")
		cmd := exec.Command("go", "install", tool.Repo+"@"+tool.Version)
		cmd.Env = append(os.Environ(), "GOBIN="+gobin)
		output, err := cmd.CombinedOutput()
		logger.Debug("[DEBUG] go install output: %s\n", output)
		if err != nil {
			logger.Error("[ERROR] Go install failed: %v\n", err)
			return false, ""
		}
		return true, filepath.Join(gobin, tool.Name)

	case "rustup":
		logger.Info("[INFO] Installing %s using rustup component add...\n", tool.Name)

		cmd := exec.Command("rustup", "component", "add", tool.Name)
		output, err := cmd.CombinedOutput()
		logger.Debug("[DEBUG] rustup output: %s\n", output)
		if err != nil {
			switch {
			case strings.Contains(string(output), "does not support components"):
				logger.Error("[ERROR] Rustup failed: current toolchain doesn't support components. Set a default toolchain using `rustup default stable`\n")
			case strings.Contains(string(output), "is not a component"):
				logger.Error("[ERROR] Rustup failed: '%s' is not a valid component for this toolchain\n", tool.Name)
			default:
				logger.Error("[ERROR] Rustup component add failed: %v\n", err)
			}
			return false, ""
		}

		toolchainCmd := exec.Command("rustup", "show", "active-toolchain")
		toolchainOut, err := toolchainCmd.Output()
		if err != nil {
			logger.Error("[ERROR] Failed to get rustup toolchain: %v\n", err)
			return false, ""
		}
		toolchain := strings.Fields(string(toolchainOut))[0]
		logger.Info("[INFO] Detected rustup toolchain: %s\n", toolchain)

		actualBinaryPath := filepath.Join(os.Getenv("HOME"), ".rustup", "toolchains", toolchain, "bin", tool.Name)
		if _, err := os.Stat(actualBinaryPath); os.IsNotExist(err) {
			logger.Error("[ERROR] Expected binary %s not found after installation\n", actualBinaryPath)
			return false, ""
		}

		symlinkPath := filepath.Join(os.Getenv("HOME"), ".cargo", "bin", tool.Name)
		if _, err := os.Stat(filepath.Dir(symlinkPath)); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(symlinkPath), 0755); err != nil {
				logger.Error("[ERROR] Failed to create symlink directory: %v\n", err)
				return false, ""
			}
		}
		_ = os.Remove(symlinkPath)

		if err := os.Symlink(actualBinaryPath, symlinkPath); err != nil {
			logger.Error("[ERROR] Failed to create symlink for %s: %v\n", tool.Name, err)
			return false, ""
		}

		logger.Info("[INFO] Symlinked %s to %s\n", actualBinaryPath, symlinkPath)
		return true, symlinkPath

	default:
		logger.Warn("[WARN] Unknown tool source for %s. Skipping.\n", tool.Name)
		return false, ""
	}

	return true, installPath
}
