//go:build windows

package core

import (
	"os/exec"
	"strconv"
)

// configureProcess is a no-op on Windows: process groups are not the mechanism
// there, and taskkill /T walks the tree instead.
func configureProcess(*exec.Cmd) {}

// terminateProcess kills the child and everything it spawned.
//
// A plain Process.Kill() would leave ffmpeg running after a cancelled merge, so
// this uses `taskkill /T /F`, which is the Windows equivalent of signalling a
// Unix process group. force is accepted to match the Unix signature: there is
// no polite SIGTERM equivalent here that console apps honour, so the first call
// already kills the tree rather than orphaning children for the grace period.
func terminateProcess(cmd *exec.Cmd, _ bool) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
