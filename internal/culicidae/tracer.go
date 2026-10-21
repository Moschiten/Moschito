package culicidae

import (
	"fmt"
	"os"
	"syscall"
)

func Tracer(args []string, env []string, workdir string) error {
	pid, err := syscall.ForkExec(args[0], args, &syscall.ProcAttr{
		Dir:   workdir,
		Env:   env,
		Files: []uintptr{os.Stdin.Fd(), os.Stdout.Fd(), os.Stderr.Fd()},
		Sys:   &syscall.SysProcAttr{Ptrace: true},
	})
	if err != nil {
		return fmt.Errorf("fork failed: %w", err)
	}

	var ws syscall.WaitStatus
	_, err = syscall.Wait4(pid, &ws, 0, nil)
	if err != nil {
		return fmt.Errorf("wait4 failed: %w", err)
	}

	err = syscall.PtraceSetOptions(pid, syscall.PTRACE_O_TRACESYSGOOD|syscall.PTRACE_O_TRACEFORK|syscall.PTRACE_O_TRACEVFORK|syscall.PTRACE_O_TRACECLONE|syscall.PTRACE_O_TRACEEXEC)
	if err != nil {
		return fmt.Errorf("ptrace set options failed: %w", err)
	}

	// Track all active PIDs and whether they are entering or exiting a syscall
	activePids := map[int]bool{pid: true}
	isEntry := map[int]bool{pid: true}

	bannedSyscalls := map[uint64]bool{
		435: true, // clone3
		439: true, // faccessat2
		440: true, // process_madvise
		441: true, // epoll_pwait2
		449: true, // futex_waitv
	}

	for len(activePids) > 0 {
		// Resume every tracked PID individually
		for p := range activePids {
			syscall.PtraceSyscall(p, 0)
		}

		// Wait for any child to stop
		stoppedPid, err := syscall.Wait4(-1, &ws, syscall.WALL, nil)
		if err != nil {
			if err == syscall.ECHILD {
				break 
			}
			return fmt.Errorf("wait4 failed: %w", err)
		}

		// If we don't know this PID, ignore it
		if !activePids[stoppedPid] {
			continue
		}

		if ws.Exited() || ws.Signaled() {
			delete(activePids, stoppedPid)
			delete(isEntry, stoppedPid)
			continue
		}

		// Handle new child processes (forks, clones) and execve
                if ws.StopSignal() == syscall.SIGTRAP {
	          if ws.TrapCause() == syscall.PTRACE_EVENT_CLONE || ws.TrapCause() == syscall.PTRACE_EVENT_FORK || ws.TrapCause() == syscall.PTRACE_EVENT_VFORK {
		    msg, err := syscall.PtraceGetEventMsg(stoppedPid)
		    if err == nil {
		      newPid := int(msg)
		      activePids[newPid] = true
	  	      isEntry[newPid] = true
		    }
	          } else if ws.TrapCause() == syscall.PTRACE_EVENT_EXEC {
		    // CRITICAL FIX: Reset the entry tracker after execve
		    isEntry[stoppedPid] = true 
	          }
	          continue
                }

		// Handle syscall stops
		if ws.StopSignal() == syscall.SIGTRAP|0x80 {
			var regs syscall.PtraceRegs
			err := syscall.PtraceGetRegs(stoppedPid, &regs)
			if err != nil {
				continue
			}

			if isEntry[stoppedPid] {
				syscallNum := regs.Regs[8]
				if bannedSyscalls[syscallNum] {
					regs.Regs[8] = ^uint64(0) // Set to -1 (ENOSYS)
					syscall.PtraceSetRegs(stoppedPid, &regs)
				}
				isEntry[stoppedPid] = false
			} else {
				isEntry[stoppedPid] = true
			}
		}
	}
	return nil
}
