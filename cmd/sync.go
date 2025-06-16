package cmd

import (
	"github.com/spf13/cobra"
	"setup-machine/internal/config"
	"setup-machine/internal/installer"
)

// configPath holds the path to the main configuration YAML file.
// It's passed via the `--config` or `-c` flag.
var configPath string

// statePath is the path to the persistent state file.
// This file tracks applied settings and installed tools.
var statePath = "state.json" // You can make this configurable too

// syncCmd is the top-level command for syncing all configuration aspects:
// tools, macOS settings, shell aliases, and fonts.
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync system state with config (tools, settings, aliases, fonts)",
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration and state
		cfg := config.LoadConfig(configPath)
		st := config.LoadState(statePath)

		// Sync tools, settings, aliases, and fonts
		installer.SyncTools(cfg.Tools, st)
		installer.SyncSettings(cfg.Settings, st)
		installer.SyncAliases(cfg.Aliases)
		installer.SyncFonts(cfg.Fonts, st)

		// Save updated state after syncing
		config.SaveState(statePath, st)
	},
}

// syncToolsCmd syncs only the tool installations.
var syncToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Sync only tools with config",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.LoadConfig(configPath)
		st := config.LoadState(statePath)

		installer.SyncTools(cfg.Tools, st)
		config.SaveState(statePath, st)
	},
}

// syncSettingsCmd syncs only macOS settings.
var syncSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Sync only macOS settings with config",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.LoadConfig(configPath)
		st := config.LoadState(statePath)

		installer.SyncSettings(cfg.Settings, st)
		config.SaveState(statePath, st)
	},
}

// syncAliasesCmd syncs only shell aliases.
var syncAliasesCmd = &cobra.Command{
	Use:   "aliases",
	Short: "Sync only shell aliases with config",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.LoadConfig(configPath)
		installer.SyncAliases(cfg.Aliases)
	},
}

// syncFontsCmd syncs only fonts.
var syncFontsCmd = &cobra.Command{
	Use:   "fonts",
	Short: "Sync only fonts with config",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.LoadConfig(configPath)
		st := config.LoadState(statePath)

		installer.SyncFonts(cfg.Fonts, st)
		config.SaveState(statePath, st)
	},
}

// init sets up CLI flags and adds subcommands to the root command.
func init() {
	// Global flag for specifying config file path
	syncCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")

	// Add subcommands for more granular control
	syncCmd.AddCommand(syncToolsCmd)
	syncCmd.AddCommand(syncSettingsCmd)
	syncCmd.AddCommand(syncAliasesCmd)
	syncCmd.AddCommand(syncFontsCmd)
	// Register the `sync` command with the root command
	rootCmd.AddCommand(syncCmd)
}
