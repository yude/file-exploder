package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "file-exploder",
	Short: "FileExploder - Remote file operation queue manager",
	Long:  "A server-side daemon for managing queued file operations over SSH.",
	// The commands are driven over SSH and their stdout is parsed as JSON, so a
	// failure should be one line on stderr: main already prints the error, and a
	// usage dump only buries it.
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}
