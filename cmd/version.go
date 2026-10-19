package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "1.0000"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of Moschito",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("moschito version %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
