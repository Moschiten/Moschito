package culicidae

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

//go:embed midge-linux-amd64
var midgeAMD64 []byte

//go:embed midge-linux-arm64
var midgeARM64 []byte

//go:embed libfakechroot.so
var fakechrootSO []byte

type Backend interface {
	Execute(rootfs, workdir string, command []string, env map[string]string) error
	Name() string
}

func DetectBackend() Backend {
	if runtime.GOOS == "android" || os.Getenv("TERMUX_VERSION") != "" {
		fmt.Println("-> [Host] Android detected. Using Go ptrace Tracer + Midge + Fakechroot.")
		return &TracerBackend{}
	}
	fmt.Println("-> [Host] Linux detected. Using Native MFE + Midge.")
	return &NativeBackend{}
}

func injectMidge(rootfs string) error {
	var midgeData []byte
	if runtime.GOARCH == "arm64" {
		midgeData = midgeARM64
	} else {
		midgeData = midgeAMD64
	}
	midgeDir := filepath.Join(rootfs, ".moschito")
	os.MkdirAll(midgeDir, 0755)
	
	targetPath := filepath.Join(midgeDir, "midge")
	os.Remove(targetPath) 
	
	return os.WriteFile(targetPath, midgeData, 0755)
}

// --- Tracer Backend (Android) ---
type TracerBackend struct{}

func (t *TracerBackend) Name() string { return "go-tracer" }

func (t *TracerBackend) Execute(rootfs, workdir string, command []string, env map[string]string) error {
	if err := injectMidge(rootfs); err != nil {
		return err
	}

	// Inject fakechroot shared library
	soPath := filepath.Join(rootfs, ".moschito", "libfakechroot.so")
	if err := os.WriteFile(soPath, fakechrootSO, 0755); err != nil {
		return err
	}

	midgePath := filepath.Join(rootfs, ".moschito", "midge")
	args := append([]string{midgePath}, command...)

	var cleanEnv []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LD_PRELOAD=") {
			cleanEnv = append(cleanEnv, e)
		}
	}
	cleanEnv = append(cleanEnv, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	cleanEnv = append(cleanEnv, "FAKECHROOT_BASE="+rootfs)
	cleanEnv = append(cleanEnv, "MOSCHITO_WORKDIR="+workdir)
	
	// Set LD_PRELOAD to the HOST path so the glibc linker finds it
	cleanEnv = append(cleanEnv, "LD_PRELOAD="+soPath)

	for k, v := range env {
		cleanEnv = append(cleanEnv, fmt.Sprintf("%s=%s", k, v))
	}

	hostWorkdir := rootfs
	if workdir != "" && workdir != "/" {
		hostWorkdir = filepath.Join(rootfs, workdir)
		os.MkdirAll(hostWorkdir, 0755) 
	}

	return Tracer(args, cleanEnv, hostWorkdir)
}

// --- Native Linux Backend ---
type NativeBackend struct{}
func (n *NativeBackend) Name() string { return "native-mfe" }

func (n *NativeBackend) Execute(rootfs, workdir string, command []string, env map[string]string) error {
	if err := injectMidge(rootfs); err != nil {
		return err
	}
	midgeArgs := append([]string{"/.moschito/midge"}, command...)
	cmd := exec.Command(midgeArgs[0], midgeArgs[1:]...)
	
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC | syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
		Chroot:     rootfs,
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	if workdir != "" && workdir != "/" {
		cmd.Dir = workdir
	}
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
