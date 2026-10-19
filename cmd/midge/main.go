package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("midge: no command provided")
		os.Exit(1)
	}

	binary := os.Args[1]
	args := os.Args[2:]

	resolvedPath, err := resolvePath(binary)
	if err != nil {
		fmt.Printf("midge: failed to resolve %s: %v\n", binary, err)
		os.Exit(1)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	cmd := exec.Command(resolvedPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		fmt.Printf("midge: failed to start %s: %v\n", resolvedPath, err)
		os.Exit(1)
	}

	go func() {
		for sig := range sigs {
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

func resolvePath(path string) (string, error) {
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
			path = target
		}
	}
	return "", fmt.Errorf("too many levels of symbolic links")
}
