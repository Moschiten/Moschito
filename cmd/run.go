package cmd

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moschiten/moschito/internal/culicidae"
	"github.com/moschiten/moschito/internal/hive"
	"github.com/moschiten/moschito/internal/instar"
	"github.com/spf13/cobra"
)

type RunConfig struct {
	Config struct {
		Env        []string `json:"Env"`
		Entrypoint []string `json:"Entrypoint"`
		Cmd        []string `json:"Cmd"`
	} `json:"config"`
}

var runCmd = &cobra.Command{
	Use:   "run [image]",
	Short: "Run a container from an image in the Hive",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		imageName := args[0]
		safeName := strings.ReplaceAll(imageName, ":", "-")
		safeName = strings.ReplaceAll(safeName, "/", "-")

		h := hive.New()
		imageTar := filepath.Join(h.ImageDir, safeName+".tar")

		if _, err := os.Stat(imageTar); os.IsNotExist(err) {
			fmt.Printf("Image %s not found in Hive. Run 'moschito build' first.\n", imageName)
			os.Exit(1)
		}

		runDir := filepath.Join(h.BuildDir, "run-session")
		os.RemoveAll(runDir)
		os.MkdirAll(runDir, 0755)

		fmt.Printf("Unpacking %s to runtime environment...\n", imageName)
		extractImage(imageTar, runDir)

		configData, _ := os.ReadFile(filepath.Join(runDir, "config.json"))
		var config RunConfig
		json.Unmarshal(configData, &config)

		// Combine Entrypoint and Cmd for execution
		execCmd := append(config.Config.Entrypoint, config.Config.Cmd...)
		if len(execCmd) == 0 {
			fmt.Println("No CMD or ENTRYPOINT defined in image.")
			os.Exit(1)
		}

		fmt.Printf("Executing: %v\n", execCmd)

		envMap := make(map[string]string)
		for _, e := range config.Config.Env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}

		rootfsPath := filepath.Join(runDir, "rootfs")
		backend := culicidae.DetectBackend()
		
		if err := backend.Execute(rootfsPath, "/", execCmd, envMap); err != nil {
			fmt.Printf("Execution failed: %v\n", err)
			os.Exit(1)
		}
	},
}

// Extracts the outer OCI tar and the inner layer.tar
func extractImage(tarPath, dest string) {
	f, err := os.Open(tarPath)
	if err != nil { return }
	defer f.Close()
	tr := tar.NewReader(f)

	for {
		header, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { break }

		target := filepath.Join(dest, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil { continue }
			io.Copy(out, tr)
			out.Close()
		}
		
		// Extract the rootfs
		if header.Name == "layer.tar" {
			rootfsPath := filepath.Join(dest, "rootfs")
			os.MkdirAll(rootfsPath, 0755)
			instar.ExtractLayer(target, rootfsPath)
		}
	}
}

func init() {
	rootCmd.AddCommand(runCmd)
}
