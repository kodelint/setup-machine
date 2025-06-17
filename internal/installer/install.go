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

// installTool attempts to install a CLI tool based on the configuration provided.
// It supports multiple installation sources such as GitHub releases, URLs, Homebrew, Go, and Rustup.
// Returns a boolean indicating success, and the path where the tool was installed (if successful).
func installTool(tool config.Tool) (bool, string) {
	// Log debug info about the tool and source to be installed.
	config.Debug("[DEBUG] installTool: Installing tool %s from source %s\n", tool.Name, tool.Source)

	var installPath string // path where the tool binary is installed or placed
	var err error          // for capturing errors during installation steps

	// Determine installation method based on the tool's Source field.
	switch tool.Source {

	// GitHub installation: download from GitHub releases/assets.
	case "github":
		config.Info("[INFO] Installing %s@%s from GitHub...\n", tool.Name, tool.Version)
		installPath, err = downloadToolsFromGitHub(tool) // handles downloading and extracting
		if err != nil {
			config.Error("[ERROR] Failed to install %s from GitHub: %v\n", tool.Name, err)
			return false, ""
		}

	// Custom URL installation, can be .pkg installers or archives.
	case "url":
		config.Info("[INFO] Installing %s from custom URL...\n", tool.Name)
		// Temporary download path in /tmp folder.
		tmp := "/tmp/" + path.Base(tool.URL)

		// Use curl to download the file from the URL to the temporary location.
		curlCmd := exec.Command("curl", "-L", tool.URL, "-o", tmp)
		config.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
		output, err := curlCmd.CombinedOutput()
		if err != nil {
			// If curl fails, log error including command output for troubleshooting.
			config.Error("[ERROR] Download failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
			return false, ""
		}

		// Check if the downloaded file is a macOS installer package (.pkg)
		if strings.HasSuffix(tool.URL, ".pkg") {
			config.Info("[INFO] Detected .pkg file for %s. Installing via macOS installer...\n", tool.Name)

			// Run macOS installer command to install the .pkg system-wide
			installCmd := exec.Command("sudo", "installer", "-pkg", tmp, "-target", "/")
			config.Debug("[DEBUG] Running command: %s\n", strings.Join(installCmd.Args, " "))
			output, err = installCmd.CombinedOutput()
			if err != nil {
				// Log failure to install the .pkg
				config.Error("[ERROR] .pkg installation failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}

			// .pkg installs apps mostly under /Applications, returning that general location.
			return true, "/Applications"

		} else {
			// Otherwise treat as archive: extract the file, find executable, and chmod +x
			asset, err := extractAndInstall(tmp, "/tmp/")
			if err != nil {
				return false, ""
			}

			config.Debug("[DEBUG] Extracted asset to %s\n", asset)

			// Make sure the extracted asset is executable.
			chmodCmd := exec.Command("chmod", "+x", asset)
			config.Debug("[DEBUG] Running command: %s\n", strings.Join(chmodCmd.Args, " "))
			output, err = chmodCmd.CombinedOutput()
			if err != nil {
				config.Error("[ERROR] chmod failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
				return false, ""
			}

			// Record the install path as the executable's path.
			installPath = asset
		}

	// Homebrew installation for macOS packages managed by brew.
	case "brew":
		config.Info("[INFO] Installing %s using Homebrew...\n", tool.Name)

		// Use arch -arm64 to ensure brew installs for Apple Silicon arch.
		cmd := exec.Command("arch", "-arm64", "brew", "install", tool.Name)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] brew install output: %s\n", output)
		if err != nil {
			config.Error("[ERROR] Brew install failed: %v\n", err)
			return false, ""
		}

		// Return standard Homebrew binary path for Apple Silicon
		return true, "/opt/homebrew/bin/" + tool.Name

	// Installation via `go install` for Go tools.
	case "go":
		config.Info("[INFO] Installing %s using go install...\n", tool.Name)

		// GOBIN environment variable directs where to install the binary.
		gobin := filepath.Join(os.Getenv("HOME"), "go", "bin")

		// Run `go install repo@version` to fetch and build the tool.
		cmd := exec.Command("go", "install", tool.Repo+"@"+tool.Version)
		cmd.Env = append(os.Environ(), "GOBIN="+gobin) // override GOBIN
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] go install output: %s\n", output)
		if err != nil {
			config.Error("[ERROR] Go install failed: %v\n", err)
			return false, ""
		}

		// Return the expected binary path inside $HOME/go/bin/
		return true, filepath.Join(gobin, tool.Name)

	// Installation via rustup components for Rust tools.
	case "rustup":
		config.Info("[INFO] Installing %s using rustup component add...\n", tool.Name)

		// Run rustup to add the specified component/tool.
		cmd := exec.Command("rustup", "component", "add", tool.Name)
		output, err := cmd.CombinedOutput()
		config.Debug("[DEBUG] rustup output: %s\n", output)
		if err != nil {
			// Handle known rustup errors with tailored messages.
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

		// Determine the active rustup toolchain name (e.g. stable-x86_64-apple-darwin)
		toolchainCmd := exec.Command("rustup", "show", "active-toolchain")
		toolchainOut, err := toolchainCmd.Output()
		if err != nil {
			config.Error("[ERROR] Failed to get rustup toolchain: %v\n", err)
			return false, ""
		}
		toolchain := strings.Fields(string(toolchainOut))[0]
		config.Info("[INFO] Detected rustup toolchain: %s\n", toolchain)

		// Construct the expected path of the installed binary inside rustup directory.
		actualBinaryPath := filepath.Join(os.Getenv("HOME"), ".rustup", "toolchains", toolchain, "bin", tool.Name)
		if _, err := os.Stat(actualBinaryPath); os.IsNotExist(err) {
			// If the binary isn't found, report failure.
			config.Error("[ERROR] Expected binary %s not found after installation\n", actualBinaryPath)
			return false, ""
		}

		// Ensure ~/.cargo/bin exists as the location for symlinks.
		symlinkPath := filepath.Join(os.Getenv("HOME"), ".cargo", "bin", tool.Name)
		if _, err := os.Stat(filepath.Dir(symlinkPath)); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(symlinkPath), 0755); err != nil {
				config.Error("[ERROR] Failed to create symlink directory: %v\n", err)
				return false, ""
			}
		}

		// Remove any existing symlink before creating a new one.
		_ = os.Remove(symlinkPath)

		// Create a symlink pointing from ~/.cargo/bin/<tool> to the rustup installed binary.
		if err := os.Symlink(actualBinaryPath, symlinkPath); err != nil {
			config.Error("[ERROR] Failed to create symlink for %s: %v\n", tool.Name, err)
			return false, ""
		}

		config.Info("[INFO] Symlinked %s to %s\n", actualBinaryPath, symlinkPath)
		return true, symlinkPath

	// Unknown source type: log a warning and skip.
	default:
		config.Warn("[WARN] Unknown tool source for %s. Skipping.\n", tool.Name)
		return false, ""
	}

	// Return success and installation path if reached here (usually for github/url cases).
	return true, installPath
}

// installFont downloads and installs font files from a provided URL to the user's Fonts directory.
// It filters to only install fonts that are "Regular" style and have .ttf or .otf extensions.
// Returns a list of installed font file paths or an error.
func installFont(fontName, url string) ([]string, error) {
	// Create a temporary directory for downloading and extracting the font archive.
	tmpDir, err := os.MkdirTemp("", "font-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	// Ensure temporary directory is cleaned up after function exits.
	defer os.RemoveAll(tmpDir)

	// Construct the path where the zip archive will be downloaded.
	archivePath := filepath.Join(tmpDir, fontName+".zip")

	// Download the font archive zip file from the given URL.
	if err := downloadFile(url, archivePath); err != nil {
		return nil, fmt.Errorf("failed to download font archive: %w", err)
	}

	// Extract the downloaded zip archive into a subdirectory.
	extractDir := filepath.Join(tmpDir, "unzipped")
	_, err = extractZip(archivePath, extractDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract zip archive: %w", err)
	}

	config.Debug("[DEBUG] Extracted font archive to: %s\n", extractDir)

	// Create the destination Fonts directory inside user's home Library folder.
	fontDir := filepath.Join(os.Getenv("HOME"), "Library", "Fonts")
	if err := os.MkdirAll(fontDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create Fonts dir: %w", err)
	}

	installedFiles := []string{} // list to keep track of installed font file paths

	// Walk through extracted files to find suitable font files for installation.
	err = filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		// Skip if error encountered or this is a directory.
		if err != nil || info.IsDir() {
			return nil
		}

		// Check file name lowercase for "regular" and font file extensions .ttf or .otf
		lowerName := strings.ToLower(info.Name())
		if strings.Contains(lowerName, "regular") &&
			(strings.HasSuffix(lowerName, ".ttf") || strings.HasSuffix(lowerName, ".otf")) {

			// Destination path for the font file in the system Fonts directory.
			dst := filepath.Join(fontDir, info.Name())

			// Copy the font file from extraction folder to Fonts directory.
			if copyErr := copyFile(path, dst, 0); copyErr != nil {
				config.Warn("[WARN] Failed to copy %s to %s: %v\n", path, dst, copyErr)
				return nil // continue with other files despite error
			}

			// Append the installed font file path to the list.
			installedFiles = append(installedFiles, dst)
			config.Debug("[DEBUG] Installed font file: %s\n", dst)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error while installing fonts: %w", err)
	}

	// Warn if no suitable "Regular" fonts were found in the archive.
	if len(installedFiles) == 0 {
		config.Warn("[WARN] No 'Regular' fonts found in %s\n", url)
	}

	// Return the list of installed font files to the caller.
	return installedFiles, nil
}
