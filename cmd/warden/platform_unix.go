//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func pidFilePath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "warden.pid")
	}
	return fmt.Sprintf("/tmp/warden-%d.pid", os.Getuid())
}

func notifyStopSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

func isRestartSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP
}

func restartNeedsPIDFileRemoval() bool {
	return false
}

func restartCurrentProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}
	return syscall.Exec(executable, os.Args, os.Environ())
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func sendReloadSignal() error {
	pid, err := readPidFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("warden is not running (no pid file at %s)", pidFile)
		}
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("send SIGHUP to process %d: %w", pid, err)
	}
	return nil
}
