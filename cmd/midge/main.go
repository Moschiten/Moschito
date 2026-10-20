package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("midge: no command provided")
		os.Exit(1)
	}

	binary := os.Args[1]
	args := os.Args[2:]
	base := os.Getenv("FAKECHROOT_BASE")

	var finalPath string
	var finalArgs []string

	if base != "" {
		resolvedPath, err := resolvePath(binary, base)
		if err != nil {
			fmt.Printf("midge: failed to resolve %s: %v\n", binary, err)
			os.Exit(1)
		}

		linker := findLinker(base)
		if linker != "" {
			libPath := fmt.Sprintf("%s/.moschito:%s/lib:%s/usr/lib:%s/lib64:%s/usr/lib64:%s/lib/aarch64-linux-gnu:%s/usr/lib/aarch64-linux-gnu:%s/lib/x86_64-linux-gnu:%s/usr/lib/x86_64-linux-gnu", base, base, base, base, base, base, base, base, base)
			finalPath = linker
			finalArgs = append([]string{linker, "--library-path", libPath, resolvedPath}, args...)
		} else {
			finalPath = resolvedPath
			finalArgs = append([]string{resolvedPath}, args...)
		}

		wd := os.Getenv("MOSCHITO_WORKDIR")
		if wd != "" && wd != "/" {
			os.Chdir(filepath.Join(base, wd))
		} else {
			os.Chdir(base)
		}
	} else {
		resolvedPath, err := resolvePath(binary, "")
		if err != nil {
			fmt.Printf("midge: failed to resolve %s: %v\n", binary, err)
			os.Exit(1)
		}
		finalPath = resolvedPath
		finalArgs = append([]string{resolvedPath}, args...)
		
		wd := os.Getenv("MOSCHITO_WORKDIR")
		if wd != "" && wd != "/" {
			os.Chdir(wd)
		}
	}

	// Filter out Termux's host LD_PRELOAD but KEEP our fakechroot
	var cleanEnv []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "LD_PRELOAD=") {
			val := strings.TrimPrefix(e, "LD_PRELOAD=")
			var filtered []string
			for _, lib := range strings.Split(val, ":") {
				if !strings.Contains(lib, "libtermux-exec") {
					filtered = append(filtered, lib)
				}
			}
			if len(filtered) > 0 {
				cleanEnv = append(cleanEnv, "LD_PRELOAD="+strings.Join(filtered, ":"))
			}
		} else {
			cleanEnv = append(cleanEnv, e)
		}
	}

	// Replace the current process with the target binary
	err := syscall.Exec(finalPath, finalArgs, cleanEnv)
	if err != nil {
		fmt.Printf("midge: exec failed: %v\n", err)
		os.Exit(1)
	}
}

func resolvePath(path, base string) (string, error) {
	if base != "" && filepath.IsAbs(path) && !strings.HasPrefix(path, base) {
		path = filepath.Join(base, path)
	}
	if !filepath.IsAbs(path) {
		lookedUp, err := exec.LookPath(path)
		if err != nil {
			return "", err
		}
		path = lookedUp
	}
	for i := 0; i < 10; i++ {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			path = filepath.Join(filepath.Dir(path), target)
		} else {
			if base != "" {
				path = filepath.Join(base, target)
			} else {
				path = target
			}
		}
	}
	return "", fmt.Errorf("too many levels of symbolic links")
}

func findLinker(base string) string {
	candidates := []string{
		"lib/ld-linux-aarch64.so.1",
		"lib64/ld-linux-x86-64.so.2",
		"lib/ld-linux.so.2",
		"lib/ld-musl-aarch64.so.1",
	}
	for _, c := range candidates {
		p := filepath.Join(base, c)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
