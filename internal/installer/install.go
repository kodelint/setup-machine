package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"setup-machine/internal/config"
	"setup-machine/internal/logger"
	"strings"
)

// installTool installs a tool based on its source configuration.
// It returns a boolean indicating success and the final install path of the tool.
func installTool(tool config.Tool) (bool, string) {
	logger.Debug("[DEBUG] installTool: Installing tool %s from source %s\n", tool.Name, tool.Source)

	var installPath string
	var err error

	switch tool.Source {
	case "github":
		// Install tool by fetching release from GitHub
		logger.Info("[INFO] Installing %s@%s from GitHub...\n", tool.Name, tool.Version)
		installPath, err = downloadFromGitHub(tool)
		if err != nil {
			logger.Error("[ERROR] Failed to install %s from GitHub: %v\n", tool.Name, err)
			return false, ""
		}

	case "url":
		// Install tool from a custom URL (e.g. GitHub Releases, company server, etc.)
		logger.Info("[INFO] Installing %s from custom URL...\n", tool.Name)
		tmp := "/tmp/" + tool.Name

		// Download the binary using curl
		curlCmd := exec.Command("curl", "-L", tool.URL, "-o", tmp)
		logger.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
		output, err := curlCmd.CombinedOutput()
		if err != nil {
			logger.Error("[ERROR] Download failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
			return false, ""
		}

		// Make the downloaded file executable
		chmodCmd := exec.Command("chmod", "+x", tmp)
		logger.Debug("[DEBUG] Running command: %s\n", strings.Join(chmodCmd.Args, " "))
		output, err = chmodCmd.CombinedOutput()
		if err != nil {
			logger.Error("[ERROR] chmod failed for %s: %v\nOutput: %s\n", tool.Name, err, output)
			return false, ""
		}

		// Move the binary to a proper location (e.g. /usr/local/bin or ~/bin)
		installPath, err = installExecutable(tmp, tool.Name)
		if err != nil {
			logger.Error("[ERROR] installing executable failed for %s: %v", tool.Name, err)
			return false, ""
		}

	default:
		// Unsupported source type
		logger.Warn("[WARN] Unknown tool source for %s. Skipping.\n", tool.Name)
		return false, ""
	}

	// Return successful installation
	return true, installPath
}

// copyFile copies a file from src to dst, preserving file permissions.
func copyFile(src, dst string) error {
	logger.Debug("[DEBUG] Copying file from %s to %s\n", src, dst)

	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer func() {
		if cerr := srcFile.Close(); cerr != nil {
			logger.Warn("[WARN] Failed to close source file: %v\n", cerr)
		}
	}()

	// Get file permissions of source
	stat, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file %s: %w", src, err)
	}

	// Open the destination file with the same permissions
	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("failed to open destination file %s: %w", dst, err)
	}
	defer func() {
		if cerr := dstFile.Close(); cerr != nil {
			logger.Warn("[WARN] Failed to close destination file: %v\n", cerr)
		}
	}()

	// Copy contents from source to destination
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy data to %s: %w", dst, err)
	}
	return nil
}

// getUserLocalBin returns the path to the user's ~/bin directory.
// Creates it if it doesn't exist. This is a fallback if /usr/local/bin can't be written.
func getUserLocalBin() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	binPath := filepath.Join(usr.HomeDir, "bin")

	// Create the directory if it doesn't exist
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		logger.Debug("[DEBUG] Creating user local bin directory: %s\n", binPath)
		err = os.Mkdir(binPath, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create user local bin directory %s: %w", binPath, err)
		}
	}
	return binPath, nil
}

// installExecutable installs an executable file to the appropriate system or user binary directory.
// It tries /usr/local/bin first, then falls back to ~/bin if permission is denied.
func installExecutable(srcPath, toolName string) (string, error) {
	targetDir := "/usr/local/bin"
	targetPath := filepath.Join(targetDir, toolName)
	logger.Debug("[DEBUG] Installing executable %s to %s\n", srcPath, targetPath)

	// Attempt to copy to /usr/local/bin
	err := copyFile(srcPath, targetPath)
	if err != nil {
		// If permission is denied, fall back to ~/bin
		if os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			logger.Warn("[WARN] Permission denied writing to %s, falling back to user local bin\n", targetDir)

			binPath, err := getUserLocalBin()
			if err != nil {
				logger.Error("[ERROR] Failed to get or create user local bin: %v\n", err)
				return "", err
			}
			targetPath = filepath.Join(binPath, toolName)
			err2 := copyFile(srcPath, targetPath)
			if err2 != nil {
				logger.Error("[ERROR] Copy to user local bin failed: %v\n", err2)
				return "", err2
			}
			logger.Info("[INFO] Installed %s to user local bin: %s\n", toolName, targetPath)
			logger.Info("[INFO] Make sure to add %s to your PATH if it's not already included\n", binPath)
			return targetPath, nil
		}

		// Other error during copy
		logger.Error("[ERROR] Copy failed: %v\n", err)
		return "", err
	}

	// Successful install to /usr/local/bin
	logger.Info("[INFO] Installed %s to %s\n", toolName, targetPath)
	return targetPath, nil
}
