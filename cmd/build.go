package cmd

import (
	"fmt"
	"os"

	"github.com/moschiten/msc/parser"
	"github.com/spf13/cobra"
)

var file string
var testOnly bool

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build an image from a .msc script",
	Run: func(cmd *cobra.Command, args []string) {
		if file == "" {
			fmt.Println("Error: must provide a script file with -f")
			os.Exit(1)
		}

		// Calling parser.ParseScript instead of msc.ParseScript
		instructions, err := parser.ParseScript(file)
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Script is valid (found %d instructions).\n", len(instructions))

		if testOnly {
			fmt.Println("Dry-run complete. Exiting.")
			return
		}

		fmt.Println("--- Instructions ---")
		for _, inst := range instructions {
			fmt.Printf("Line %d: [%s] %v\n", inst.Line, inst.Command, inst.Args)
		}
	},
}

func init() {
	buildCmd.Flags().StringVarP(&file, "file", "f", "build.msc", "Path to the .msc script file")
	buildCmd.Flags().BoolVarP(&testOnly, "test", "t", false, "Validate the script without executing")
	rootCmd.AddCommand(buildCmd)
}
