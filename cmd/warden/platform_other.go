//go:build !unix && !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	return fmt.Errorf("process restart is unsupported on this platform")
}

func processAlive(pid int) bool {
	_ = pid
	return false
}

func sendReloadSignal() error {
	if _, err := readPidFile(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("warden is not running (no pid file at %s)", pidFile)
		}
		return err
	}
	return fmt.Errorf("reload via -r is unsupported on this platform")
}
