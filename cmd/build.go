package cmd

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moschiten/msc/parser"
	"github.com/moschiten/moschito/internal/culicidae"
	"github.com/moschiten/moschito/internal/hive"
	"github.com/moschiten/moschito/internal/instar"
	"github.com/moschiten/moschito/internal/proboscis"
	"github.com/spf13/cobra"
)

var file string
var testOnly bool

type OciConfig struct {
	Architecture string           `json:"architecture"`
	OS           string           `json:"os"`
	Config       OciRuntimeConfig `json:"config"`
}

type OciRuntimeConfig struct {
	Env          []string            `json:"Env,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	User         string              `json:"User,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
}

type BuildContext struct {
	Name       string
	RootfsPath string
	WorkDir    string
	Env        map[string]string
	Cmd        []string
	Entrypoint []string
	User       string
	Ports      []string
	Labels     map[string]string
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build an image from a .msc script",
	Run: func(cmd *cobra.Command, args []string) {
		if file == "" {
			fmt.Println("Error: must provide a script file with -f")
			os.Exit(1)
		}

		instructions, err := parser.ParseScript(file)
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Script is valid (%d instructions found).\n\n", len(instructions))
		if testOnly {
			fmt.Println("Dry-run complete. Exiting.")
			return
		}

		fmt.Println("--- Executing Instructions ---")
		vars := make(map[string]string)
		contexts := make(map[string]*BuildContext)
		var currentCtx *BuildContext

		h := hive.New()
		sessionRoot := filepath.Join(h.BuildDir, "session-1")
		os.RemoveAll(sessionRoot)
		os.MkdirAll(sessionRoot, 0755)

		for _, inst := range instructions {
			args := inst.Args
			switch inst.Command {
			case "ARCH":
				fmt.Printf("Target architecture: %s\n", args[0])

			case "VAR", "ENV":
				parts := strings.SplitN(args[0], "=", 2)
				if len(parts) == 2 {
					key := parts[0]
					val := interpolate(parts[1], vars)
					vars[key] = val
					if inst.Command == "ENV" && currentCtx != nil {
						currentCtx.Env[key] = val
					}
					fmt.Printf("Set %s: %s=%s\n", inst.Command, key, val)
				}

			case "PULL":
				image := interpolate(args[0], vars)
				ctxName := "default"
				if len(args) >= 3 && args[1] == "TO" {
					ctxName = args[2]
				}

				registry := "registry-1.docker.io"
				repo := image
				if !strings.Contains(repo, "/") {
					repo = "library/" + repo
				}
				if strings.Contains(repo, "/") && strings.Contains(repo, ".") && strings.Index(repo, ".") < strings.Index(repo, "/") {
					parts := strings.SplitN(repo, "/", 2)
					registry = parts[0]
					repo = parts[1]
				}

				cacheDir := filepath.Join(h.BaseDir, "hive")
				client := proboscis.NewClient(registry, cacheDir)

				fmt.Printf("Executing PULL %s TO %s\n", repo, ctxName)
				manifest, err := client.PullImage(repo)
				if err != nil {
					fmt.Printf("Error: Failed to pull image: %v\n", err)
					os.Exit(1)
				}

				rootfsPath := filepath.Join(sessionRoot, ctxName)
				os.MkdirAll(rootfsPath, 0755)

				for _, layer := range manifest.Layers {
					digest := strings.TrimPrefix(layer.Digest, "sha256:")
					blobPath := filepath.Join(cacheDir, digest)

					fmt.Printf("-> Extracting layer %s...\n", digest[:12])
					if err := instar.ExtractLayer(blobPath, rootfsPath); err != nil {
						fmt.Printf("Error: Failed to extract layer: %v\n", err)
						os.Exit(1)
					}
				}

				currentCtx = &BuildContext{
					Name: ctxName, RootfsPath: rootfsPath, WorkDir: "/",
					Env: make(map[string]string), Labels: make(map[string]string),
				}
				contexts[ctxName] = currentCtx
				fmt.Printf("-> Context '%s' ready\n", ctxName)

			case "EMPTY":
				ctxName := "default"
				if len(args) >= 2 && args[0] == "TO" {
					ctxName = args[1]
				}
				rootfsPath := filepath.Join(sessionRoot, ctxName)
				os.MkdirAll(rootfsPath, 0755)
				currentCtx = &BuildContext{
					Name: ctxName, RootfsPath: rootfsPath, WorkDir: "/",
					Env: make(map[string]string), Labels: make(map[string]string),
				}
				contexts[ctxName] = currentCtx
				fmt.Printf("-> Created EMPTY context: %s\n", ctxName)

			case "PIVOT":
				if ctx, ok := contexts[args[0]]; ok {
					currentCtx = ctx
					fmt.Printf("-> Pivoted to: %s\n", args[0])
				} else {
					fmt.Printf("Error: Context '%s' not found\n", args[0])
					os.Exit(1)
				}

			case "CP":
				if currentCtx == nil { os.Exit(1) }
				var src, dest string
				if strings.HasPrefix(args[0], "--from=") {
					fromCtx := strings.TrimPrefix(args[0], "--from=")
					src = filepath.Join(contexts[fromCtx].RootfsPath, args[1])
					dest = filepath.Join(currentCtx.RootfsPath, args[2])
				} else {
					// Handles CP source destination OR CP source TO destination
					src = args[0]
					destPart := args[1]
					if len(args) >= 3 && args[1] == "TO" {
						destPart = args[2]
					}
					dest = filepath.Join(currentCtx.RootfsPath, destPart)
				}
				if err := instar.CopyPath(src, dest); err != nil {
					fmt.Printf("Error: Failed to copy: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("-> Copied %s to %s\n", src, dest)

			case "DIR":
				if currentCtx != nil {
					currentCtx.WorkDir = args[0]
					fmt.Printf("-> Set working dir: %s\n", args[0])
				}

			case "RUN":
				if currentCtx == nil { os.Exit(1) }
				cmdArray := parseCommand(inst.Args[0])
				fmt.Printf("-> RUN %v\n", cmdArray)

				backend := culicidae.DetectBackend()
				if err := backend.Execute(currentCtx.RootfsPath, currentCtx.WorkDir, cmdArray, currentCtx.Env); err != nil {
					fmt.Printf("RUN failed: %v\n", err)
					os.Exit(1)
				}

			case "CMD":
				if currentCtx != nil {
				   fullCmd := strings.Join(inst.Args, " ")
				   currentCtx.Cmd = parseCommand(fullCmd)
				}

			case "POINT":
				if currentCtx != nil {
				   fullCmd := strings.Join(inst.Args, " ")
			           currentCtx.Entrypoint = parseCommand(fullCmd)
                                }

			case "USR":
				if currentCtx != nil { currentCtx.User = args[0] }

			case "PORT":
				if currentCtx != nil { currentCtx.Ports = append(currentCtx.Ports, args[0]) }

			case "LABEL":
				if currentCtx != nil {
					parts := strings.SplitN(args[0], "=", 2)
					if len(parts) == 2 { currentCtx.Labels[parts[0]] = parts[1] }
				}

			case "DONE":
				if currentCtx == nil { os.Exit(1) }
				imageName := interpolate(args[0], vars)
				safeName := strings.ReplaceAll(imageName, ":", "-")
				safeName = strings.ReplaceAll(safeName, "/", "-")

				fmt.Printf("-> Packaging image: %s\n", imageName)

				config := OciConfig{
					Architecture: "amd64",
					OS:           "linux",
					Config: OciRuntimeConfig{
						User:         currentCtx.User,
						Labels:       currentCtx.Labels,
						Cmd:          currentCtx.Cmd,
						Entrypoint:   currentCtx.Entrypoint,
						ExposedPorts: make(map[string]struct{}),
					},
				}
				for k, v := range currentCtx.Env {
					config.Config.Env = append(config.Config.Env, k+"="+v)
				}
				for _, p := range currentCtx.Ports {
					config.Config.ExposedPorts[p+"/tcp"] = struct{}{}
				}

				configBytes, _ := json.MarshalIndent(config, "", "  ")
				os.WriteFile(filepath.Join(sessionRoot, "config.json"), configBytes, 0644)

				fmt.Println("-> Creating layer.tar...")
				layerPath := filepath.Join(sessionRoot, "layer.tar")
				if err := tarDirectory(currentCtx.RootfsPath, layerPath); err != nil {
					fmt.Printf("Failed to tar rootfs: %v\n", err)
					os.Exit(1)
				}

				manifest := []map[string]interface{}{
					{
						"Config":   "config.json",
						"RepoTags": []string{imageName},
						"Layers":   []string{"layer.tar"},
					},
				}
				manifestBytes, _ := json.Marshal(manifest)
				os.WriteFile(filepath.Join(sessionRoot, "manifest.json"), manifestBytes, 0644)

				finalTar := filepath.Join(h.ImageDir, safeName+".tar")
				packageDockerImage(sessionRoot, finalTar)

				fmt.Printf("\nSUCCESS: Built %s\n", imageName)
				fmt.Printf("Image stored in Hive: %s\n", finalTar)
				return

			default:
				fmt.Printf("Skipping %s (not yet implemented)\n", inst.Command)
			}
		}
	},
}

func interpolate(s string, vars map[string]string) string {
	return os.Expand(s, func(key string) string {
		if val, ok := vars[key]; ok { return val }
		return "${" + key + "}"
	})
}

func parseCommand(cmdStr string) []string {
	cmdStr = strings.TrimSpace(cmdStr)

	// 1. Check for JSON array format: ["cmd", "arg"]
	if strings.HasPrefix(cmdStr, "[") && strings.HasSuffix(cmdStr, "]") {
		var result []string
		if err := json.Unmarshal([]byte(cmdStr), &result); err == nil {
			return result
		}
	}

	// 2. Fall back to shell-style parsing
	var args []string
	var current strings.Builder
	inQuotes := false
	var quoteChar rune

	for _, r := range cmdStr {
		if r == '"' || r == '\'' {
			if inQuotes && r == quoteChar {
				inQuotes = false
				continue
			} else if !inQuotes {
				inQuotes = true
				quoteChar = r
				continue
			}
		}
		if r == ' ' && !inQuotes {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func tarDirectory(src, dest string) error {
	f, err := os.Create(dest)
	if err != nil { return err }
	defer f.Close()
	tw := tar.NewWriter(f)
	defer tw.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil { return err }
		
		relPath, err := filepath.Rel(src, path)
		if err != nil { return err }
		if relPath == "." { return nil } // Skip the root dir itself

		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, _ = os.Readlink(path)
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil { return err }
		
		header.Name = relPath
		
		if err := tw.WriteHeader(header); err != nil { return err }
		
		// ONLY copy data if it is a regular file (avoid directories and symlinks)
		if info.Mode().IsRegular() {
			data, err := os.Open(path)
			if err != nil { return err }
			defer data.Close()
			if _, err := io.Copy(tw, data); err != nil { return err }
		}
		return nil
	})
}

func packageDockerImage(dir, outPath string) {
	f, err := os.Create(outPath)
	if err != nil { return }
	defer f.Close()
	tw := tar.NewWriter(f)
	defer tw.Close()

	files := []string{"manifest.json", "config.json", "layer.tar"}
	for _, file := range files {
		path := filepath.Join(dir, file)
		info, err := os.Stat(path)
		if err != nil { continue }
		
		header, err := tar.FileInfoHeader(info, "")
		if err != nil { continue }
		header.Name = file
		tw.WriteHeader(header)
		
		data, err := os.Open(path)
		if err != nil { continue }
		io.Copy(tw, data)
		data.Close()
	}
}

func init() {
	buildCmd.Flags().StringVarP(&file, "file", "f", "build.msc", "Path to the .msc script file")
	buildCmd.Flags().BoolVarP(&testOnly, "test", "t", false, "Validate the script without executing")
	rootCmd.AddCommand(buildCmd)
}
