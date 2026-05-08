package install

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wweir/warden/config"
)

// ConfirmFunc prompts the user for yes/no confirmation.
type ConfirmFunc func(label string) bool

// PasswordPromptFunc prompts the user for a sensitive password value.
type PasswordPromptFunc func(label string) (string, bool)

const windowsManagedRestartExitCode = 75

const (
	managedLocalAddr     = "127.0.0.1:9832"
	managedExternalAddr  = ":9832"
	defaultAdminPassword = "admin"
	managedConfigMode    = 0o600
)

const minimumManagedAdminPasswordLength = 12

type Options struct {
	Confirm             ConfirmFunc
	AdminPassword       *string
	AdminPasswordPrompt PasswordPromptFunc
	StartAfterInstall   *bool
	ExposeExternally    *bool
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

func ShouldExposeManagedService(opts Options) bool {
	if opts.ExposeExternally != nil {
		return *opts.ExposeExternally
	}
	if opts.Confirm != nil {
		return opts.Confirm(`Bootstrap config can listen only on localhost (safer default) or on all network interfaces.
Choose "yes" only when clients on other machines must connect directly to this host.
External exposure binds Warden to port 9832 on all interfaces.
If a new config is created, interactive install prompts for a strong admin password.
Non-interactive external bootstrap keeps the admin UI disabled until you set admin_password.
Expose Warden on all network interfaces?`)
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

func managedBootstrapConfig(exposeExternally bool, adminPassword string) string {
	addr := managedLocalAddr
	adminBlock := disabledAdminBlock(exposeExternally)
	if adminPassword != "" {
		adminBlock = enabledAdminBlock(exposeExternally, adminPassword)
	}
	if exposeExternally {
		addr = managedExternalAddr
	}
	return fmt.Sprintf(`addr = %q

%s

# Add provider and route sections before exposing model traffic.
`, addr, adminBlock)
}

func enabledAdminBlock(exposeExternally bool, adminPassword string) string {
	url := "http://localhost:9832/_admin/"
	exposureNote := "# Change this password before exposing Warden beyond trusted local access."
	if exposeExternally {
		url = "http://<host>:9832/_admin/"
		exposureNote = "# This admin UI is reachable from the network; use only a strong private password."
	}
	return fmt.Sprintf(`# Admin UI: %s
# Username is "admin"; password was set during installation.
%s
admin_password = %q`, url, exposureNote, config.EncodeSecret(adminPassword))
}

func disabledAdminBlock(exposeExternally bool) string {
	if exposeExternally {
		return `# Admin UI stays disabled in the external non-interactive bootstrap config.
# Set a strong admin_password before enabling remote admin access.
# Example:
# admin_password = "replace-with-a-strong-secret"`
	}
	return `# Admin UI stays disabled until admin_password is set.
# Example:
# admin_password = "replace-with-a-strong-secret"`
}

func resolveManagedAdminPassword(exposeExternally bool, opts Options) (string, error) {
	if opts.AdminPassword != nil {
		if err := ValidateManagedAdminPassword(*opts.AdminPassword); err != nil {
			return "", err
		}
		return *opts.AdminPassword, nil
	}

	if opts.AdminPasswordPrompt != nil {
		password, ok := opts.AdminPasswordPrompt("Enter admin password for Warden Admin UI")
		if !ok {
			return "", fmt.Errorf("admin password prompt was canceled")
		}
		if err := ValidateManagedAdminPassword(password); err != nil {
			return "", err
		}
		return password, nil
	}

	if exposeExternally {
		return "", nil
	}
	return defaultAdminPassword, nil
}

// ValidateManagedAdminPassword checks the managed install admin password policy.
func ValidateManagedAdminPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("admin password cannot be empty")
	}
	if utf8.RuneCountInString(password) < minimumManagedAdminPasswordLength {
		return fmt.Errorf("admin password must be at least %d characters", minimumManagedAdminPasswordLength)
	}
	return nil
}

func ensureManagedBootstrapConfig(configPath string, opts Options) (bool, error) {
	if _, err := os.Stat(configPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect default config path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	exposeExternally := ShouldExposeManagedService(opts)
	adminPassword, err := resolveManagedAdminPassword(exposeExternally, opts)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(configPath, []byte(managedBootstrapConfig(exposeExternally, adminPassword)), managedConfigMode); err != nil {
		return false, fmt.Errorf("write default config: %w", err)
	}
	fmt.Printf("  Created default config: %s\n", configPath)
	if exposeExternally {
		fmt.Println("  Bootstrap config listens on all network interfaces via port 9832")
		if adminPassword == "" {
			fmt.Println("  Admin UI is disabled until admin_password is set in the config")
		} else {
			fmt.Println(`  Admin UI username is "admin"; use the password entered during installation`)
			fmt.Println("  Admin UI is reachable on exposed interfaces; keep this password strong and private")
		}
	} else {
		fmt.Println("  Bootstrap config listens on localhost only: http://localhost:9832/_admin/")
		if opts.AdminPasswordPrompt != nil || opts.AdminPassword != nil {
			fmt.Println(`  Admin UI username is "admin"; use the password entered during installation`)
		} else {
			fmt.Println(`  Admin UI username is "admin"; update admin_password before exposing Warden`)
		}
	}
	return true, nil
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
