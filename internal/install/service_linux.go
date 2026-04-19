//go:build linux

package install

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	linuxServiceName = "warden"
	linuxBinaryPath  = "/usr/local/bin/warden"
	linuxConfigPath  = "/etc/warden.yaml"
	linuxServicePath = "/etc/systemd/system/warden.service"
)

var runSystemctl = systemctl

func InstallService(opts Options) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing systemd service requires root privileges, try running with sudo")
	}

	execPath, err := currentExecutablePath()
	if err != nil {
		return err
	}

	isUpdate := linuxServiceIsInstalled()
	isActive := linuxServiceIsActive()
	isEnabled := linuxServiceIsEnabled()
	if execPath != linuxBinaryPath {
		if isActive {
			_ = runSystemctl("stop", linuxServiceName)
		}
		if err := copyBinary(execPath, linuxBinaryPath); err != nil {
			return err
		}
		fmt.Printf("  Binary copied to %s\n", linuxBinaryPath)
		execPath = linuxBinaryPath
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Warden AI Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s -c %s
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, execPath, linuxConfigPath)

	if err := os.WriteFile(linuxServicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	fmt.Printf("  Service file installed: %s\n", linuxServicePath)

	ensureExampleConfig(linuxConfigPath)

	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Println("  Systemd daemon reloaded")

	if isUpdate {
		return finishLinuxUpdate(opts, isActive, isEnabled)
	}
	return finishLinuxInstall(opts)
}

func finishLinuxInstall(opts Options) error {
	if !ShouldStartAfterInstall(opts, "Start and enable warden service now?") {
		fmt.Println()
		fmt.Println("Warden service installed. Next steps:")
		fmt.Printf("  1. Edit %s to configure providers and routes\n", linuxConfigPath)
		fmt.Printf("  2. sudo systemctl start %s\n", linuxServiceName)
		fmt.Printf("  3. sudo systemctl enable %s\n", linuxServiceName)
		return nil
	}

	if err := runSystemctl("enable", linuxServiceName); err != nil {
		return err
	}
	if err := runSystemctl("start", linuxServiceName); err != nil {
		return err
	}
	fmt.Println("  Service enabled and started")
	return nil
}

func finishLinuxUpdate(opts Options, isActive, isEnabled bool) error {
	if !ShouldStartAfterInstall(opts, "Restart warden service now?") {
		fmt.Println()
		fmt.Println("Warden service updated. To apply:")
		if isActive {
			fmt.Printf("  sudo systemctl restart %s\n", linuxServiceName)
			return nil
		}
		fmt.Printf("  sudo systemctl start %s\n", linuxServiceName)
		if !isEnabled {
			fmt.Printf("  sudo systemctl enable %s\n", linuxServiceName)
		}
		return nil
	}

	if !isEnabled {
		if err := runSystemctl("enable", linuxServiceName); err != nil {
			return err
		}
	}
	if isActive {
		if err := runSystemctl("restart", linuxServiceName); err != nil {
			return err
		}
		fmt.Println("  Service restarted")
		return nil
	}
	if err := runSystemctl("start", linuxServiceName); err != nil {
		return err
	}
	fmt.Println("  Service enabled and started")
	return nil
}

func linuxServiceIsInstalled() bool {
	return fileExists(linuxServicePath)
}

func linuxServiceIsActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", linuxServiceName).Run() == nil
}

func linuxServiceIsEnabled() bool {
	return exec.Command("systemctl", "is-enabled", "--quiet", linuxServiceName).Run() == nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s: %w - %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func managedConfigPath() string {
	return linuxConfigPath
}
