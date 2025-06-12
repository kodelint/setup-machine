package config

import (
	"gopkg.in/yaml.v3"
	"os"
)

// Config is the top-level structure returned after loading all YAML configurations.
// It contains parsed data for tools, macOS settings, and shell aliases.
type Config struct {
	Tools    []Tool
	Settings []Setting
	Aliases  Aliases
}

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
// - Entries: List of aliases to apply.
//type Aliases struct {
//	Shell   string  `yaml:"shell"`
//	Entries []Alias `yaml:"entries"`
//}

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

// LoadConfig reads the main config.yaml file and the three referenced sub-configs:
// tools.yaml, settings.yaml, and aliases.yaml. It returns a populated Config struct.
func LoadConfig(configFile string) Config {
	// mainConfig holds the paths to tools, settings, and aliases config files
	mainConfig := struct {
		Config struct {
			ToolsFile    string `yaml:"tools_file"`
			SettingsFile string `yaml:"settings_file"`
			AliasesFile  string `yaml:"aliases_file"`
		} `yaml:"config"`
	}{}

	// Read and parse the main config.yaml which holds metadata (paths to other YAMLs)
	raw, err := os.ReadFile(configFile)
	if err != nil {
		panic("Failed to read config.yaml: " + err.Error())
	}
	if err := yaml.Unmarshal(raw, &mainConfig); err != nil {
		panic("Failed to unmarshal config.yaml: " + err.Error())
	}

	// ----- Load tools.yaml -----
	toolsData, err := os.ReadFile(mainConfig.Config.ToolsFile)
	if err != nil {
		panic("Failed to read tools.yaml: " + err.Error())
	}
	var toolsWrapper struct {
		Tools []Tool `yaml:"tools"`
	}
	if err := yaml.Unmarshal(toolsData, &toolsWrapper); err != nil {
		panic("Failed to unmarshal tools.yaml: " + err.Error())
	}

	// ----- Load settings.yaml -----
	// This expects the structure: settings: { macos: [ {domain, key, value, type}, ... ] }
	settingsData, err := os.ReadFile(mainConfig.Config.SettingsFile)
	if err != nil {
		panic("Failed to read settings.yaml: " + err.Error())
	}
	var settingsWrapper struct {
		Settings struct {
			MacOS []Setting `yaml:"macos"`
		} `yaml:"settings"`
	}
	if err := yaml.Unmarshal(settingsData, &settingsWrapper); err != nil {
		panic("Failed to unmarshal settings.yaml: " + err.Error())
	}

	// ----- Load aliases.yaml -----
	aliasesData, err := os.ReadFile(mainConfig.Config.AliasesFile)
	if err != nil {
		panic("Failed to read aliases.yaml: " + err.Error())
	}
	var aliasesWrapper struct {
		Aliases Aliases `yaml:"aliases"`
	}
	if err := yaml.Unmarshal(aliasesData, &aliasesWrapper); err != nil {
		panic("Failed to unmarshal aliases.yaml: " + err.Error())
	}

	// Assemble and return the full config object
	return Config{
		Tools:    toolsWrapper.Tools,
		Settings: settingsWrapper.Settings.MacOS,
		Aliases:  aliasesWrapper.Aliases,
	}
}
