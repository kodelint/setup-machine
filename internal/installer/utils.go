package installer

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"setup-machine/internal/config"
	"strings"
)

// downloadFile downloads the content located at the specified URL and saves it to the destination path.
// It returns an error if the download or file write fails.
func downloadFile(url, destPath string) error {
	// Make an HTTP GET request to the given URL
	resp, err := http.Get(url)
	if err != nil {
		// Wrap and return the error with context
		return fmt.Errorf("failed to GET %s: %w", url, err)
	}
	// Ensure the response body stream is closed when the function returns,
	// avoiding resource leaks.
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// Log error if closing response body fails, but do not return it
			config.Error("[ERROR] Failed to close response body: %s\n", cerr)
		}
	}()

	// Create or truncate the file at destPath to write the downloaded content
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", destPath, err)
	}
	// Ensure the file is closed after writing to flush contents and release resources
	defer func() {
		if cerr := out.Close(); cerr != nil {
			config.Error("[ERROR] Failed to close destination file: %s\n", cerr)
		}
	}()

	// Copy the entire response body (downloaded data) into the destination file
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write response to file: %w", err)
	}

	// Log debug message confirming successful download and file location
	config.Debug("[DEBUG] Downloaded font zip to: %s\n", destPath)
	return nil
}

// detectShell tries to figure out which shell the current user is using by reading the
// SHELL environment variable. It currently supports detection of zsh and bash,
// returning "zsh" as a default fallback if the shell is unknown or unsupported.
func detectShell() string {
	// Read the SHELL environment variable (e.g., /bin/bash, /bin/zsh)
	shell := os.Getenv("SHELL")
	config.Debug("[DEBUG] Detected shell environment: %s\n", shell)

	// Check if the shell string contains "zsh"
	if strings.Contains(shell, "zsh") {
		return "zsh"
	} else if strings.Contains(shell, "bash") { // Check for bash
		return "bash"
	}
	// If shell is neither bash nor zsh, default to zsh
	return "zsh"
}

// globbingMatches takes a slice of file paths (matches) and attempts to remove each one
// using the 'sudo rm -f' command. This is used for removing binaries or other files
// that may require elevated permissions.
// Returns true if any files were successfully removed, false otherwise.
func globbingMatches(matches []string) bool {
	result := false // Track if any file was removed successfully

	// Iterate over all matched file paths
	for _, match := range matches {
		config.Info("[INFO] Removing matched binary: %s\n", match)

		// Execute 'sudo rm -f <match>' to forcibly delete the file
		cmd := exec.Command("sudo", "rm", "-f", match)
		output, err := cmd.CombinedOutput() // Capture both stdout and stderr

		// Check if command succeeded
		if err != nil {
			// Log error with command output if removal failed
			config.Error("[ERROR] Failed to remove %s: %v\nOutput: %s\n", match, err, output)
		} else {
			// Log success and mark result as true
			config.Info("[INFO] Successfully removed %s\n", match)
			result = true
		}
	}

	// Return whether any file was successfully removed
	return result
}

// copyFile copies a file from src to dst, preserving permissions.
// It creates any missing directories in the destination path.
// Returns an error if any step in the process fails.

func copyFile(src, dst string, modeOverride os.FileMode) error {
	// Open the source file
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source failed: %w", err)
	}
	defer in.Close()

	// Ensure the destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

	// Create the destination file with write permission (mode doesn't matter yet)
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create target failed: %w", err)
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	// Copy contents
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	// Set permissions: use override if provided, otherwise preserve source mode
	if modeOverride != 0 {
		err = os.Chmod(dst, modeOverride)
	} else if stat, err2 := os.Stat(src); err2 == nil {
		err = os.Chmod(dst, stat.Mode())
	}
	return err
}
