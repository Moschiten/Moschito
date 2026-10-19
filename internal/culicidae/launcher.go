package culicidae

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

//go:embed midge-linux-arm64
var midgeBinary []byte

type Backend interface {
	Execute(rootfs, workdir string, command []string, env map[string]string) error
	Name() string
}

func DetectBackend() Backend {
	if runtime.GOOS == "android" || os.Getenv("TERMUX_VERSION") != "" {
		if _, err := exec.LookPath("proot"); err == nil {
			fmt.Println("-> [Host] Using proot to launch Midge")
			return &ProotBackend{}
		}
	}
	fmt.Println("-> [Host] Using native MFE to launch Midge")
	return &NativeBackend{}
}

// injectMidge drops the static binary into the container
func injectMidge(rootfs string) error {
	midgeDir := filepath.Join(rootfs, ".moschito")
	os.MkdirAll(midgeDir, 0755)
	targetPath := filepath.Join(midgeDir, "midge")
	return os.WriteFile(targetPath, midgeBinary, 0755)
}

// --- Native Backend ---
type NativeBackend struct{}
func (n *NativeBackend) Name() string { return "native-mfe" }

func (n *NativeBackend) Execute(rootfs, workdir string, command []string, env map[string]string) error {
	if err := injectMidge(rootfs); err != nil {
		return err
	}

	// Execute Midge, passing the user's command to it
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

// --- Proot Backend ---
type ProotBackend struct{}
func (p *ProotBackend) Name() string { return "proot" }

func (p *ProotBackend) Execute(rootfs, workdir string, command []string, env map[string]string) error {
	if err := injectMidge(rootfs); err != nil {
		return err
	}

	args := []string{"--kill-on-exit", "--link2symlink", "-0", "-r", rootfs}
	if workdir != "" && workdir != "/" {
		args = append(args, "-w", workdir)
	} else {
		args = append(args, "-w", "/")
	}
	args = append(args, "-b", "/dev", "-b", "/proc", "-b", "/sys")
	for k, v := range env {
		args = append(args, "-E", fmt.Sprintf("%s=%s", k, v))
	}
	
	// Tell proot to ONLY run Midge
	args = append(args, "/.moschito/midge")
	args = append(args, command...)

	cmd := exec.Command("proot", args...)
	cmd.Env = append(os.Environ(), "PROOT_NO_SECCOMP=1", "PROOT_TMP_DIR="+os.Getenv("PREFIX")+"/tmp")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
