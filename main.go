package main

import (
	"setup-machine/cmd" // Import the cmd package which contains the CLI commands and execution logic
)

// main is the program entry point.
// It delegates to cmd.Execute() which handles command line argument parsing and execution.
//
// This design cleanly separates the CLI interface (cmd package) from main,
// allowing easier testing, extension, and reuse of the CLI commands.
//
// The setup-machine project is a developer workstation setup automation tool that:
//   - Reads a YAML configuration file describing tools, versions, shell aliases, and macOS settings to apply
//   - Syncs installed tools by installing, upgrading, or uninstalling to match the config
//   - Installs tools primarily by downloading binaries from GitHub releases or custom URLs,
//     managing executable placement directly rather than relying on package managers like Homebrew
//   - Applies macOS system settings using the `defaults` command-line tool
//   - Adds shell aliases for user convenience
//   - Maintains a JSON state file to track which tools and settings have been applied,
//     enabling idempotent and incremental runs (only applying changes when necessary)
//
// Error handling strategy:
//   - Logs errors extensively to provide visibility into issues while continuing execution where safe,
//     aiming to apply as many configuration changes as possible in one run
//   - Fatal errors in command execution cause the program to exit with a non-zero status,
//     ensuring user is notified of critical failures
//
// Integration points:
//   - Directly manages tool installations by fetching release assets or binaries and copying them to appropriate bin directories
//   - Uses macOS native `defaults` utility to read/write system preferences
//   - Supports multiple shells (zsh, bash) by detecting the user shell or respecting config,
//     and appends shell aliases to the relevant shell RC files
//   - Tracks persistent state locally to avoid redundant actions in subsequent runs
//
// This modular and source-driven design provides flexibility to support new tools or platforms
// without relying on external package managers, improving reproducibility and control over installs.
func main() {
	cmd.Execute()
}
