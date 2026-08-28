package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var statCmd = &cobra.Command{
	Use:   "stat [path]",
	Short: "Get file info safely as JSON",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStat,
}

var statPathBase64 string

func init() {
	statCmd.Flags().StringVar(&statPathBase64, "path-base64", "", "Path encoded as UTF-8 Base64")
	rootCmd.AddCommand(statCmd)
}

func runStat(cmd *cobra.Command, args []string) error {
	plainPath := ""
	if len(args) == 1 {
		plainPath = args[0]
	}
	target, err := decodePathFlag("path", plainPath, statPathBase64)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("a path or --path-base64 is required")
	}

	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !utf8.ValidString(target) || !utf8.ValidString(info.Name()) {
		return fmt.Errorf("path cannot be represented safely as UTF-8")
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(newFileInfo(target, info.Name(), info))
}
