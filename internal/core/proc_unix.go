//go:build !windows

package core

import (
	"os/exec"
	"syscall"
)

// configureProcess puts the child in its own process group so the whole tree —
// yt-dlp plus any ffmpeg or aria2c it spawns — can be signalled at once.
func configureProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcess signals the child's process group. force escalates from
// SIGTERM to SIGKILL once killGracePeriod has passed.
//
// A negative pid means "the whole group", which is the entire reason for
// configureProcess. ESRCH simply means the process already exited.
func terminateProcess(cmd *exec.Cmd, force bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}
