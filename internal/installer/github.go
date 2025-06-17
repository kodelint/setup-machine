package installer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path"
	"runtime"
	"setup-machine/internal/config"
	"strings"
)

// downloadToolsFromGitHub downloads a specific version of a tool from GitHub Releases.
// It locates the asset matching the OS/Arch, downloads it, extracts the archive,
// finds the executable, installs it, and returns the installed path.
func downloadToolsFromGitHub(tool config.Tool) (string, error) {
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
	config.Debug("[DEBUG] Fetching GitHub release from URL: %s\n", url)

	// Make HTTP request to GitHub API
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("HTTP GET Error fetching release for %s@%s: %w", tool.Name, tool.Version, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			config.Warn("[WARN] Failed to close HTTP response body: %v\n", cerr)
		}
	}()

	// Handle non-200 responses
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub release fetch failed for %s@%s: HTTP status %d", tool.Name, tool.Version, resp.StatusCode)
	}

	// Parse the JSON response into the GitHubRelease struct
	var release config.GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode GitHub release JSON for %s@%s: %w", tool.Name, tool.Version, err)
	}
	config.Debug("[DEBUG] Release tag: %s with %d assets\n", release.TagName, len(release.Assets))

	// Detect local OS and architecture
	arch := strings.ToLower(runtime.GOARCH)
	osys := strings.ToLower(runtime.GOOS)
	config.Debug("[DEBUG] Looking for asset matching OS=%s or macos ARCH=%s\n", osys, arch)

	// Define preferred asset filename patterns for macOS/arm64
	preferredPatterns := []string{
		"darwin_amd64", "darwin-arm64", "darwin_aarch64", "aarch64-apple-darwin", "macos", "macOS_amd64", "macos_amd64",
	}

	// Search for an asset that matches the preferred patterns
	var assetURL, assetName string
	for _, pattern := range preferredPatterns {
		for _, asset := range release.Assets {
			config.Debug("[DEBUG] Within Release Patten matching asset: %s with name: %s\n", asset.BrowserDownloadURL, asset.Name)
			assetNameLower := strings.ToLower(asset.Name)
			if strings.Contains(assetNameLower, pattern) &&
				(strings.HasSuffix(assetNameLower, ".tar.gz") ||
					strings.HasSuffix(assetNameLower, ".tgz") ||
					strings.HasSuffix(assetNameLower, ".tar.bz2") ||
					strings.HasSuffix(assetNameLower, ".tar.xz") ||
					strings.HasSuffix(assetNameLower, ".zip")) {
				assetURL = asset.BrowserDownloadURL
				assetName = asset.Name
				config.Debug("[DEBUG] Found matching asset: %s\n", assetName)
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
	compressedAssetName := "/tmp/" + path.Base(assetURL)
	config.Info("[INFO] Downloading asset %s to %s\n", assetName, compressedAssetName)
	curlCmd := exec.Command("curl", "-L", assetURL, "-o", compressedAssetName)
	config.Debug("[DEBUG] Running command: %s\n", strings.Join(curlCmd.Args, " "))
	output, err := curlCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to download asset %s: %v\nOutput: %s", assetName, err, output)
	}

	// Extract the downloaded archive
	asset, err := extractAndInstall(compressedAssetName, "/tmp/")
	if err != nil {
		return "", fmt.Errorf("failed to extract archive: %v", err)
	}

	config.Debug("[DEBUG] Extracted asset to %s\n", asset)
	config.Info("[INFO] Installed %s \n", asset)
	return asset, nil
}
