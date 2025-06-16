package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"setup-machine/internal/config"
	"strings"
)

func installTool(tool config.Tool) (bool, string) {
	config.Debug("[DEBUG] installTool: Installing tool %s from source %s\n", tool.Name, tool.Source)

	var installPath string
	var err error

	switch tool.Source {
	case "github":
		config.Info("[INFO] Installing %s@%s from GitHub...\n", tool.Name, tool.Version)
		installPath, err = downloadFromGitHub(tool)
		if err != nil {
			config.Error("[ERROR] Failed to install %s from GitHub: %v\n", tool.Name, err)
			return false, ""
		}

	case "url":
		config.Info("[INFO] Installing %s from custom URL...\n", tool.Name)
		tmp := "/tmp/" + path.Base(tool.URL)

		// Download the file using curl
		curlCmd := exec.Command("curl", "-L", tool.URL, "-o", tmp)
		config.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
		output, err := curlCmd.CombinedOutput()
		if err != nil {
			config.Error("[ERROR] Download failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
			return false, ""
		}

		// If it's a .pkg file, install it using the macOS installer
		if strings.HasSuffix(tool.URL, ".pkg") {
			config.Info("[INFO] Detected .pkg file for %s. Installing via macOS installer...\n", tool.Name)
			installCmd := exec.Command("sudo", "installer", "-pkg", tmp, "-target", "/")
			config.Debug("[DEBUG] Running command: %s\n", strings.Join(installCmd.Args, " "))
			output, err = installCmd.CombinedOutput()
			if err != nil {
				config.Error("[ERROR] .pkg installation failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}
			return true, "/Applications" // general location for GUI apps (may vary by .pkg)

		} else {
			// Otherwise, treat as archive
			asset, err := extractAndInstall(tmp, "/tmp/")
			if err != nil {
				return false, ""
			}
			config.Debug("[DEBUG] Extracted asset to %s\n", asset)

			chmodCmd := exec.Command("chmod", "+x", asset)
			config.Debug("[DEBUG] Running command: %s\n", strings.Join(chmodCmd.Args, " "))
			output, err = chmodCmd.CombinedOutput()
			if err != nil {
				config.Error("[ERROR] chmod failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}
			installPath = asset
		}
	case "brew":
		config.Info("[INFO] Installing %s using Homebrew...\n", tool.Name)
		cmd := exec.Command("arch", "-arm64", "brew", "install", tool.Name)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] brew install output: %s\n", output)
		if err != nil {
			config.Error("[ERROR] Brew install failed: %v\n", err)
			return false, ""
		}
		return true, "/opt/homebrew/bin/" + tool.Name // assumes Homebrew path

	case "go":
		config.Info("[INFO] Installing %s using go install...\n", tool.Name)
		gobin := filepath.Join(os.Getenv("HOME"), "go", "bin")
		cmd := exec.Command("go", "install", tool.Repo+"@"+tool.Version)
		cmd.Env = append(os.Environ(), "GOBIN="+gobin)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] go install output: %s\n", output)
		if err != nil {
			config.Error("[ERROR] Go install failed: %v\n", err)
			return false, ""
		}
		return true, filepath.Join(gobin, tool.Name)

	case "rustup":
		config.Info("[INFO] Installing %s using rustup component add...\n", tool.Name)

		cmd := exec.Command("rustup", "component", "add", tool.Name)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] rustup output: %s\n", output)
		if err != nil {
			switch {
			case strings.Contains(string(output), "does not support components"):
				config.Error("[ERROR] Rustup failed: current toolchain doesn't support components. Set a default toolchain using `rustup default stable`\n")
			case strings.Contains(string(output), "is not a component"):
				config.Error("[ERROR] Rustup failed: '%s' is not a valid component for this toolchain\n", tool.Name)
			default:
				config.Error("[ERROR] Rustup component add failed: %v\n", err)
			}
			return false, ""
		}

		toolchainCmd := exec.Command("rustup", "show", "active-toolchain")
		toolchainOut, err := toolchainCmd.Output()
		if err != nil {
			config.Error("[ERROR] Failed to get rustup toolchain: %v\n", err)
			return false, ""
		}
		toolchain := strings.Fields(string(toolchainOut))[0]
		config.Info("[INFO] Detected rustup toolchain: %s\n", toolchain)

		actualBinaryPath := filepath.Join(os.Getenv("HOME"), ".rustup", "toolchains", toolchain, "bin", tool.Name)
		if _, err := os.Stat(actualBinaryPath); os.IsNotExist(err) {
			config.Error("[ERROR] Expected binary %s not found after installation\n", actualBinaryPath)
			return false, ""
		}

		symlinkPath := filepath.Join(os.Getenv("HOME"), ".cargo", "bin", tool.Name)
		if _, err := os.Stat(filepath.Dir(symlinkPath)); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(symlinkPath), 0755); err != nil {
				config.Error("[ERROR] Failed to create symlink directory: %v\n", err)
				return false, ""
			}
		}
		_ = os.Remove(symlinkPath)

		if err := os.Symlink(actualBinaryPath, symlinkPath); err != nil {
			config.Error("[ERROR] Failed to create symlink for %s: %v\n", tool.Name, err)
			return false, ""
		}

		config.Info("[INFO] Symlinked %s to %s\n", actualBinaryPath, symlinkPath)
		return true, symlinkPath

	default:
		config.Warn("[WARN] Unknown tool source for %s. Skipping.\n", tool.Name)
		return false, ""
	}

	return true, installPath
}

func installFont(fontName, url string) ([]string, error) {
	tmpDir, err := os.MkdirTemp("", "font-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, fontName+".zip")
	if err := downloadFile(url, archivePath); err != nil {
		return nil, fmt.Errorf("failed to download font archive: %w", err)
	}

	extractDir := filepath.Join(tmpDir, "unzipped")
	_, err = extractZip(archivePath, extractDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract zip archive: %w", err)
	}

	config.Debug("[DEBUG] Extracted font archive to: %s\n", extractDir)

	fontDir := filepath.Join(os.Getenv("HOME"), "Library", "Fonts")
	if err := os.MkdirAll(fontDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create Fonts dir: %w", err)
	}

	installedFiles := []string{}

	err = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		lowerName := strings.ToLower(info.Name())
		if strings.Contains(lowerName, "regular") &&
			(strings.HasSuffix(lowerName, ".ttf") || strings.HasSuffix(lowerName, ".otf")) {

			dst := filepath.Join(fontDir, info.Name())
			if copyErr := copyFile(path, dst); copyErr != nil {
				config.Warn("[WARN] Failed to copy %s to %s: %v\n", path, dst, copyErr)
				return nil
			}
			installedFiles = append(installedFiles, dst)
			config.Debug("[DEBUG] Installed font file: %s\n", dst)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error while installing fonts: %w", err)
	}

	if len(installedFiles) == 0 {
		config.Warn("[WARN] No 'Regular' fonts found in %s\n", url)
	}

	return installedFiles, nil
}
