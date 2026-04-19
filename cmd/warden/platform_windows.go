//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
)

func pidFilePath() string {
	return filepath.Join(os.TempDir(), "warden.pid")
}

func notifyStopSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}

func isRestartSignal(sig os.Signal) bool {
	return false
}

func restartNeedsPIDFileRemoval() bool {
	return true
}

func restartCurrentProcess() error {
	if os.Getenv("WARDEN_SUPERVISED") == "1" {
		return &processExitError{code: managedRestartExitCode}
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement process: %w", err)
	}
	return cmd.Process.Release()
}

func processAlive(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	text := strings.TrimSpace(string(output))
	if text == "" || strings.Contains(strings.ToLower(text), "no tasks are running") {
		return false
	}
	return strings.Contains(text, fmt.Sprintf(`,"%d",`, pid))
}

func sendReloadSignal() error {
	if _, err := readPidFile(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("warden is not running (no pid file at %s)", pidFile)
		}
		return err
	}
	return fmt.Errorf("reload via -r is unsupported on Windows; use the admin restart API or restart the scheduled task")
}
