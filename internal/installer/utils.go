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

func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to GET %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			config.Error("[ERROR] Failed to close response body: %s\n", cerr)
		}
	}()

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", destPath, err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			config.Error("[ERROR] Failed to close destination file: %s\n", cerr)
		}
	}()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write response to file: %w", err)
	}

	config.Debug("[DEBUG] Downloaded font zip to: %s\n", destPath)
	return nil
}

// detectShell attempts to identify the current user's shell by inspecting the SHELL env variable.
// Returns "zsh" or "bash" or defaults to "zsh" if unknown.
func detectShell() string {
	shell := os.Getenv("SHELL")
	config.Debug("[DEBUG] Detected shell environment: %s\n", shell)

	// Match common shell strings to either zsh or bash
	if strings.Contains(shell, "zsh") {
		return "zsh"
	} else if strings.Contains(shell, "bash") {
		return "bash"
	}
	// Default fallback
	return "zsh"
}

// globbingMatches executes sudo rm on each glob match to remove the binary.
// Returns true if any files were successfully removed.
func globbingMatches(matches []string) bool {
	result := false
	for _, match := range matches {
		config.Info("[INFO] Removing matched binary: %s\n", match)
		cmd := exec.Command("sudo", "rm", "-f", match)
		output, err := cmd.CombinedOutput()
		if err != nil {
			config.Error("[ERROR] Failed to remove %s: %v\nOutput: %s\n", match, err, output)
		} else {
			config.Info("[INFO] Successfully removed %s\n", match)
			result = true
		}
	}
	return result
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source failed: %w", err)
	}
	defer in.Close()

	// Make sure destination dir exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir failed: %w", err)
	}

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

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	if stat, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, stat.Mode())
	}

	return nil
}
