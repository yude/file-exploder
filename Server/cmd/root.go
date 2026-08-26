package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "file-exploder",
	Short: "FileExploder - Remote file operation queue manager",
	Long:  "A server-side daemon for managing queued file operations over SSH.",
}

func Execute() error {
	return rootCmd.Execute()
}
