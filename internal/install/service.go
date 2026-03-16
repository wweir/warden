package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wweir/warden/config"
)

// ConfirmFunc prompts the user for yes/no confirmation.
type ConfirmFunc func(label string) bool

func InstallService(confirm ConfirmFunc) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing systemd service requires root privileges, try running with sudo")
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	if execPath, err = filepath.EvalSymlinks(execPath); err != nil {
		return fmt.Errorf("resolve executable symlink: %w", err)
	}

	isUpdate := serviceIsActive()

	const targetPath = "/usr/local/bin/warden"
	if execPath != targetPath {
		label := fmt.Sprintf("Copy binary to %s?", targetPath)
		if isUpdate {
			label = fmt.Sprintf("Update binary at %s?", targetPath)
		}
		if confirm != nil && confirm(label) {
			data, err := os.ReadFile(execPath)
			if err != nil {
				return fmt.Errorf("read binary %s: %w", execPath, err)
			}
			if isUpdate {
				_ = systemctl("stop", "warden")
			}
			if err := os.WriteFile(targetPath, data, 0o755); err != nil {
				return fmt.Errorf("write binary to %s: %w", targetPath, err)
			}
			fmt.Printf("  ✓ Binary copied to %s\n", targetPath)
			execPath = targetPath
		}
	}

	const servicePath = "/etc/systemd/system/warden.service"
	serviceContent := fmt.Sprintf(`[Unit]
Description=Warden AI Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s -c /etc/warden.yaml
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, execPath)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	fmt.Printf("  ✓ Service file installed: %s\n", servicePath)

	ensureDefaultFiles()

	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Println("  ✓ Systemd daemon reloaded")

	if isUpdate {
		return finishUpdate(confirm)
	}
	return finishInstall(confirm)
}

func finishInstall(confirm ConfirmFunc) error {
	if confirm == nil || !confirm("Start and enable warden service now?") {
		fmt.Println()
		fmt.Println("✅ Warden service installed. Next steps:")
		fmt.Println("   1. Edit /etc/warden.yaml to configure providers and routes")
		fmt.Println("   2. sudo systemctl start warden")
		fmt.Println("   3. sudo systemctl enable warden")
		return nil
	}

	if err := systemctl("enable", "warden"); err != nil {
		return err
	}
	if err := systemctl("start", "warden"); err != nil {
		return err
	}
	fmt.Println("  ✓ Service enabled and started")
	return nil
}

func finishUpdate(confirm ConfirmFunc) error {
	if confirm == nil || !confirm("Restart warden service now?") {
		fmt.Println()
		fmt.Println("✅ Warden service updated. To apply:")
		fmt.Println("   sudo systemctl restart warden")
		return nil
	}

	if err := systemctl("restart", "warden"); err != nil {
		return err
	}
	fmt.Println("  ✓ Service restarted")
	return nil
}

func serviceIsActive() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "warden")
	return cmd.Run() == nil
}

func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s: %w - %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ensureDefaultFiles() {
	const configPath = "/etc/warden.yaml"

	if _, err := os.Stat(configPath); err == nil {
		return
	} else if !os.IsNotExist(err) {
		fmt.Printf("  ⚠️  Failed to inspect default config path: %v\n", err)
		return
	}

	if err := os.WriteFile(configPath, []byte(config.ExampleConfig), 0o644); err != nil {
		fmt.Printf("  ⚠️  Failed to write default config: %v\n", err)
		return
	}
	fmt.Printf("  ✓ Default config written to %s\n", configPath)
}
