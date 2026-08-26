package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

var statCmd = &cobra.Command{
	Use:   "stat [path]",
	Short: "Get file info safely as JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runStat,
}

func init() {
	rootCmd.AddCommand(statCmd)
}

func runStat(cmd *cobra.Command, args []string) error {
	target := args[0]
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}

	result := FileInfo{
		Name:             info.Name(),
		Path:             target,
		Size:             info.Size(),
		ModificationDate: info.ModTime().Unix(),
		IsDirectory:      info.IsDir(),
		Permissions:      uint32(info.Mode() & os.ModePerm),
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(result)
}
