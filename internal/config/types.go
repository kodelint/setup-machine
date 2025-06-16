package config

// Tool represents a CLI tool or binary to be managed by the setup tool.
// - Name: Logical name for the tool.
// - Version: Version to install.
// - Source/URL/Repo/Tag: Used for resolving installation method (e.g., GitHub, custom URL, etc.).
type Tool struct {
	Name    string
	Version string
	Source  string
	URL     string
	Repo    string
	Tag     string
}

// Setting represents a macOS `defaults` system setting.
// - Domain: macOS domain (e.g., com.apple.finder).
// - Key: Specific setting key.
// - Value: Desired setting value as a string.
// - Type: Value type ("bool", "int", "string", "float").
type Setting struct {
	Domain string
	Key    string
	Value  string
	Type   string
}

// Aliases holds shell-specific alias definitions.
// - Shell: Shell type (e.g., zsh, bash).
// - RawConfigs: Shell Commands or configuration
// - Entries: List of aliases to apply.
type Aliases struct {
	Shell      string   `yaml:"shell"`
	RawConfigs []string `yaml:"raw_configs"`
	Entries    []Alias  `yaml:"entries"`
}

// Alias defines a single shell alias (e.g., ll = ls -al).
type Alias struct {
	Name  string
	Value string
}

// Font represents a downloadable font archive (.zip or .ttf) with a name and URL.
type Font struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Source  string `yaml:"source"` // Only "github" supported
	Repo    string `yaml:"repo"`   // GitHub repo, e.g., JetBrains/JetBrainsMono
	Tag     string `yaml:"tag"`    // GitHub release tag, e.g., v2.304
}

// Config is the top-level structure returned after loading all YAML configurations.
// It contains parsed data for tools, macOS settings, and shell aliases.
type Config struct {
	Tools    []Tool
	Settings []Setting
	Aliases  Aliases
	Fonts    []Font
}

// GitHubRelease represents the structure of a GitHub release JSON response.
type GitHubRelease struct {
	TagName string `json:"tag_name"` // The release tag (e.g., v1.0.0)
	Assets  []struct {
		Name               string `json:"name"`                 // Asset filename
		BrowserDownloadURL string `json:"browser_download_url"` // Direct download URL for the asset
	} `json:"assets"`
}

// ToolState represents the saved state of an installed tool.
// It records the installed version, the full install path of the tool executable,
// and a boolean indicating whether this tool was installed by this setup system.
type ToolState struct {
	Version             string `json:"version"`                // Version string of the installed tool
	InstallPath         string `json:"install_path"`           // Absolute file system path where the tool executable is installed
	InstalledByDevSetup bool   `json:"installed_by_dev_setup"` // True if installed/managed by this setup tool, false if external/manual install
}

// SettingState represents the saved state of a macOS system setting that was applied.
// It stores the domain and key for the `defaults` system, plus the string value last applied.
type SettingState struct {
	Domain string `json:"domain"` // The domain string, e.g., "com.apple.finder"
	Key    string `json:"key"`    // The key string within that domain, e.g., "AppleShowAllFiles"
	Value  string `json:"value"`  // The value last written to that key, stored as string
}

// FontState represents a font installed on the system
type FontState struct {
	Name  string   `json:"name"`  // Font name (e.g., "JetBrainsMono")
	URL   string   `json:"url"`   // Download URL used
	Files []string `json:"files"` // List of installed font file paths
}

// State holds the entire saved state for the setup tool.
// It includes maps of installed tools and applied system settings keyed by their unique identifiers.
type State struct {
	Tools    map[string]ToolState    `json:"tools"`    // Map from tool name to its ToolState
	Settings map[string]SettingState `json:"settings"` // Map from "domain:key" string to SettingState
	Fonts    map[string]FontState    `json:"fonts"`    // Map from tool name to its FontState
}
