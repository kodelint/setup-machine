package cmd

import (
	"github.com/spf13/cobra"
	"setup-machine/internal/logger"
)

// debug flag indicates whether debug logging should be enabled.
// It can be toggled via the `--debug` command-line flag.
var debug bool

// rootCmd is the base command for the CLI tool `setup-machine`.
// It sets up the root-level CLI structure and provides global flags.
var rootCmd = &cobra.Command{
	Use:   "setup-machine",     // The name of the CLI tool
	Short: "System setup tool", // Short description shown in help output

	// PersistentPreRun is a hook that runs before any subcommand.
	// Here, we initialize the logger based on the debug flag.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logger.Init(debug) // Set up logging (verbose if --debug is true)
	},
}

// Execute initializes flags, registers subcommands, and starts the command execution.
// It's the entry point for the CLI when invoked by the user.
func Execute() {
	// Register the global --debug flag before any command is executed.
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")

	// Add the `sync` command and its subcommands (defined in sync.go)
	rootCmd.AddCommand(syncCmd)

	// Execute runs the appropriate subcommand or displays help if none is provided.
	// Errors are ignored here with `_ =` since Cobra handles them internally by default.
	_ = rootCmd.Execute()
}
