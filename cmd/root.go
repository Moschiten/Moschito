package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "moschito",
	Short: "The universal, daemonless container engine.",
	Long:  "Moschito is a from-scratch container engine designed to install on any OS and run containers for any OS.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}
