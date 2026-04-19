package install

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wweir/warden/config"
)

// ConfirmFunc prompts the user for yes/no confirmation.
type ConfirmFunc func(label string) bool

const windowsManagedRestartExitCode = 75

type Options struct {
	Confirm           ConfirmFunc
	StartAfterInstall *bool
}

func ManagedConfigPath() string {
	return managedConfigPath()
}

func ShouldStartAfterInstall(opts Options, label string) bool {
	if opts.StartAfterInstall != nil {
		return *opts.StartAfterInstall
	}
	if opts.Confirm != nil {
		return opts.Confirm(label)
	}
	return false
}

func currentExecutablePath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("get executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}
	return resolved, nil
}

func copyBinary(srcPath, targetPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("read binary %s: %w", srcPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create target dir for %s: %w", targetPath, err)
	}
	if err := os.WriteFile(targetPath, data, 0o755); err != nil {
		return fmt.Errorf("write binary to %s: %w", targetPath, err)
	}
	return nil
}

func ensureExampleConfig(configPath string) {
	if _, err := os.Stat(configPath); err == nil {
		return
	} else if !os.IsNotExist(err) {
		fmt.Printf("  Warning: inspect default config path failed: %v\n", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		fmt.Printf("  Warning: create config dir failed: %v\n", err)
		return
	}
	if err := os.WriteFile(configPath, []byte(config.ExampleConfig), 0o644); err != nil {
		fmt.Printf("  Warning: write default config failed: %v\n", err)
		return
	}
	fmt.Printf("  Created default config: %s\n", configPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func launchdPlist(label, execPath, configPath, stdoutPath, stderrPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-c</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, label, execPath, configPath, stdoutPath, stderrPath)
}

func windowsTaskScript(execPath, configPath, logPath string) string {
	return fmt.Sprintf("@echo off\r\nsetlocal\r\nset WARDEN_SUPERVISED=1\r\nset WARDEN_RESTART_EXIT_CODE=%d\r\n:restart\r\n\"%s\" -c \"%s\" >> \"%s\" 2>&1\r\nset EXIT_CODE=%%ERRORLEVEL%%\r\nif \"%%EXIT_CODE%%\"==\"%d\" goto restart\r\nif \"%%EXIT_CODE%%\"==\"0\" exit /b 0\r\ntimeout /t 5 /nobreak >nul\r\ngoto restart\r\n", windowsManagedRestartExitCode, execPath, configPath, logPath, windowsManagedRestartExitCode)
}

func tasklistHasOtherImageProcess(output, imageName string, currentPID int) bool {
	reader := csv.NewReader(strings.NewReader(output))
	reader.FieldsPerRecord = -1

	for {
		record, err := reader.Read()
		if err != nil {
			return false
		}
		if len(record) < 2 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(record[0]), imageName) {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}
		if pid != currentPID {
			return true
		}
	}
}
