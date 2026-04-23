//go:build darwin

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	darwinLaunchdLabel = "com.wweir.warden"
	darwinBinaryPath   = "/usr/local/bin/warden"
	darwinConfigPath   = "/usr/local/etc/warden.yaml"
	darwinStdoutPath   = "/usr/local/var/log/warden.log"
	darwinStderrPath   = "/usr/local/var/log/warden.err.log"
	darwinLaunchdPlist = "/Library/LaunchDaemons/com.wweir.warden.plist"
)

func InstallService(opts Options) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing launchd service requires root privileges, try running with sudo")
	}

	execPath, err := currentExecutablePath()
	if err != nil {
		return err
	}

	isUpdate := fileExists(darwinLaunchdPlist)
	if execPath != darwinBinaryPath {
		if err := copyBinary(execPath, darwinBinaryPath); err != nil {
			return err
		}
		fmt.Printf("  Binary copied to %s\n", darwinBinaryPath)
		execPath = darwinBinaryPath
	}

	if err := os.MkdirAll(filepath.Dir(darwinStdoutPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	for _, path := range []string{darwinStdoutPath, darwinStderrPath} {
		if err := ensureFile(path, 0o644); err != nil {
			return err
		}
	}

	plist := launchdPlist(darwinLaunchdLabel, execPath, darwinConfigPath, darwinStdoutPath, darwinStderrPath)
	if err := os.WriteFile(darwinLaunchdPlist, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}
	fmt.Printf("  launchd plist installed: %s\n", darwinLaunchdPlist)

	ensureManagedBootstrapConfig(darwinConfigPath, opts)

	if isUpdate {
		return finishDarwinUpdate(opts)
	}
	return finishDarwinInstall(opts)
}

func finishDarwinInstall(opts Options) error {
	if !ShouldStartAfterInstall(opts, "Load and start warden launchd service now?") {
		fmt.Println()
		fmt.Println("Warden launchd service installed. Next steps:")
		fmt.Printf("  1. Edit %s to configure providers and routes\n", darwinConfigPath)
		fmt.Printf("  2. sudo launchctl bootstrap system %s\n", darwinLaunchdPlist)
		fmt.Printf("  3. sudo launchctl enable system/%s\n", darwinLaunchdLabel)
		fmt.Printf("  4. sudo launchctl kickstart -k system/%s\n", darwinLaunchdLabel)
		return nil
	}
	if err := loadDarwinService(true); err != nil {
		return err
	}
	fmt.Println("  launchd service loaded and started")
	return nil
}

func finishDarwinUpdate(opts Options) error {
	if !ShouldStartAfterInstall(opts, "Restart warden launchd service now?") {
		fmt.Println()
		fmt.Println("Warden launchd service updated. To apply:")
		fmt.Printf("  sudo launchctl bootout system/%s\n", darwinLaunchdLabel)
		fmt.Printf("  sudo launchctl bootstrap system %s\n", darwinLaunchdPlist)
		fmt.Printf("  sudo launchctl kickstart -k system/%s\n", darwinLaunchdLabel)
		return nil
	}
	if err := loadDarwinService(true); err != nil {
		return err
	}
	fmt.Println("  launchd service restarted")
	return nil
}

func loadDarwinService(forceRestart bool) error {
	target := "system/" + darwinLaunchdLabel
	if forceRestart {
		_ = launchctl("bootout", target)
	}
	if err := launchctl("bootstrap", "system", darwinLaunchdPlist); err != nil {
		return err
	}
	if err := launchctl("enable", target); err != nil {
		return err
	}
	if err := launchctl("kickstart", "-k", target); err != nil {
		return err
	}
	return nil
}

func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl %s: %w - %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ensureFile(path string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return fmt.Errorf("create file %s: %w", path, err)
	}
	return file.Close()
}

func managedConfigPath() string {
	return darwinConfigPath
}
