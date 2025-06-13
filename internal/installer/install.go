package installer

import (
	"os/exec"
	"path"
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

	default:
		logger.Warn("[WARN] Unknown tool source for %s. Skipping.\n", tool.Name)
		return false, ""
	}

	return true, installPath
}
