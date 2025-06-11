package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"setup-machine/internal/config"
	"setup-machine/internal/logger"
	"strings"
)

// GitHubRelease represents the structure of a GitHub release JSON response.
type GitHubRelease struct {
	TagName string `json:"tag_name"` // The release tag (e.g., v1.0.0)
	Assets  []struct {
		Name               string `json:"name"`                 // Asset filename
		BrowserDownloadURL string `json:"browser_download_url"` // Direct download URL for the asset
	} `json:"assets"`
}

// downloadFromGitHub downloads a specific version of a tool from GitHub Releases.
// It locates the asset matching the OS/Arch, downloads it, extracts the archive,
// finds the executable, installs it, and returns the installed path.
func downloadFromGitHub(tool config.Tool) (string, error) {
	// Determine the GitHub repository and tag
	repo := tool.Name
	tag := "v" + tool.Version
	if tool.Repo != "" {
		repo = tool.Repo
	}
	if tool.Tag != "" {
		tag = tool.Tag
	}

	// Build GitHub API URL to fetch the release metadata
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	logger.Debug("[DEBUG] Fetching GitHub release from URL: %s\n", url)

	// Make HTTP request to GitHub API
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP GET error fetching release for %s@%s: %w", tool.Name, tool.Version, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Warn("[WARN] Failed to close HTTP response body: %v\n", cerr)
		}
	}()

	// Handle non-200 responses
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub release fetch failed for %s@%s: HTTP status %d", tool.Name, tool.Version, resp.StatusCode)
	}

	// Parse the JSON response into the GitHubRelease struct
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub release JSON for %s@%s: %w", tool.Name, tool.Version, err)
	}
	logger.Debug("[DEBUG] Release tag: %s with %d assets\n", release.TagName, len(release.Assets))

	// Detect local OS and architecture
	arch := strings.ToLower(runtime.GOARCH)
	osys := strings.ToLower(runtime.GOOS)
	logger.Debug("[DEBUG] Looking for asset matching OS=%s or macos ARCH=%s\n", osys, arch)

	// Define preferred asset filename patterns for macOS/arm64
	preferredPatterns := []string{
		"darwin-arm64", "darwin_aarch64", "aarch64-apple-darwin", "arm64", "macos",
	}

	// Search for an asset that matches the preferred patterns
	var assetURL, assetName string
	for _, pattern := range preferredPatterns {
		for _, asset := range release.Assets {
			assetNameLower := strings.ToLower(asset.Name)
			if strings.Contains(assetNameLower, pattern) &&
				(strings.HasSuffix(assetNameLower, ".tar.gz") || strings.HasSuffix(assetNameLower, ".tgz") || strings.HasSuffix(assetNameLower, ".zip")) {
				assetURL = asset.BrowserDownloadURL
				assetName = asset.Name
				logger.Debug("[DEBUG] Found matching asset: %s\n", assetName)
				break
			}
		}
		if assetURL != "" {
			break
		}
	}

	// Fail if no matching asset was found
	if assetURL == "" {
		return "", fmt.Errorf("no matching asset found for OS=%s or macos, ARCH=%s in release %s", osys, arch, release.TagName)
	}

	// Download the asset to a temporary location using curl
	tmpFile := "/tmp/" + assetName
	logger.Info("[INFO] Downloading asset %s to %s\n", assetName, tmpFile)
	curlCmd := exec.Command("curl", "-L", assetURL, "-o", tmpFile)
	logger.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
	output, err := curlCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to download asset %s: %v\nOutput: %s", assetName, err, output)
	}

	// Extract the downloaded archive
	var extractDir string
	if strings.HasSuffix(assetName, ".zip") {
		extractDir, err = unzip(tmpFile)
	} else if strings.HasSuffix(assetName, ".tar.gz") || strings.HasSuffix(assetName, ".tgz") {
		extractDir, err = untarGz(tmpFile)
	} else {
		return "", fmt.Errorf("unsupported archive format for asset: %s", assetName)
	}
	if err != nil {
		return "", fmt.Errorf("failed to extract asset %s: %w", assetName, err)
	}
	logger.Debug("[DEBUG] Extracted asset to %s\n", extractDir)

	// Find the first executable file in the extracted folder
	execPath, err := findExecutable(extractDir)
	if err != nil {
		return "", fmt.Errorf("failed to find executable in extracted files: %w", err)
	}
	logger.Debug("[DEBUG] Found executable: %s\n", execPath)

	// Install the executable to the system (usually in /usr/local/bin)
	exeBase := filepath.Base(execPath)
	installPath, err := installExecutable(execPath, exeBase)
	if err != nil {
		return "", fmt.Errorf("installation failed: %w", err)
	}

	logger.Info("[INFO] Installed %s to %s\n", exeBase, installPath)
	return installPath, nil
}

// unzip extracts a .zip file to a temporary directory and returns the extraction path.
func unzip(src string) (string, error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return "", err
	}
	defer r.Close()

	destDir := filepath.Join("/tmp", "extract-"+filepath.Base(src)+"-"+randomString(6))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	// Iterate through each file in the ZIP archive
	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return "", err
		}

		// Create file on disk and write ZIP contents to it
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return "", err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return "", err
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()

		if err != nil {
			return "", err
		}
	}
	return destDir, nil
}

// untarGz extracts a .tar.gz or .tgz archive into a temporary directory.
func untarGz(src string) (string, error) {
	file, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	destDir := filepath.Join("/tmp", "extract-"+filepath.Base(src)+"-"+randomString(6))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	// Process each file entry in the TAR archive
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return "", err
		}

		target := filepath.Join(destDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", err
			}
			outFile.Close()
		default:
			// Ignore other file types
		}
	}
	return destDir, nil
}

// findExecutable recursively searches for the first executable binary file
// in the given root directory and returns its full path.
func findExecutable(root string) (string, error) {
	logger.Debug("[DEBUG] Searching for executable under %s\n", root)
	var execPath string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Warn("[WARN] Error accessing path %s: %v\n", path, err)
			return nil // Skip error and continue walking
		}
		if !info.IsDir() && (info.Mode()&0111) != 0 { // Check if file is executable
			execPath = path
			logger.Debug("[DEBUG] Executable found: %s\n", execPath)
			return filepath.SkipDir // Stop once found
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("error walking directory %s: %w", root, err)
	}
	if execPath == "" {
		return "", fmt.Errorf("no executable file found in %s", root)
	}
	return execPath, nil
}
