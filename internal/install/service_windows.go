//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	windowsTaskName   = "Warden"
	windowsBinaryPath = `C:\Program Files\Warden\warden.exe`
	windowsConfigPath = `C:\ProgramData\Warden\warden.yaml`
	windowsLogPath    = `C:\ProgramData\Warden\logs\warden.log`
	windowsScriptPath = `C:\Program Files\Warden\warden-task.cmd`
)

func InstallService(opts Options) error {
	execPath, err := currentExecutablePath()
	if err != nil {
		return err
	}

	isUpdate := windowsTaskExists()
	if execPath != windowsBinaryPath {
		if err := copyBinary(execPath, windowsBinaryPath); err != nil {
			return fmt.Errorf("stage binary to %s: %w", windowsBinaryPath, err)
		}
		fmt.Printf("  Binary copied to %s\n", windowsBinaryPath)
		execPath = windowsBinaryPath
	}

	if err := os.MkdirAll(filepath.Dir(windowsLogPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	if err := ensureFile(windowsLogPath, 0o644); err != nil {
		return err
	}

	ensureExampleConfig(windowsConfigPath)

	if err := os.MkdirAll(filepath.Dir(windowsScriptPath), 0o755); err != nil {
		return fmt.Errorf("create task script dir: %w", err)
	}
	if err := os.WriteFile(windowsScriptPath, []byte(windowsTaskScript(execPath, windowsConfigPath, windowsLogPath)), 0o755); err != nil {
		return fmt.Errorf("write task wrapper script: %w", err)
	}
	fmt.Printf("  Task wrapper installed: %s\n", windowsScriptPath)

	if err := installWindowsTask(); err != nil {
		return err
	}
	fmt.Printf("  Scheduled task installed: %s\n", windowsTaskName)

	if isUpdate {
		return finishWindowsUpdate(opts)
	}
	return finishWindowsInstall(opts)
}

func finishWindowsInstall(opts Options) error {
	if !ShouldStartAfterInstall(opts, "Start Warden scheduled task now?") {
		fmt.Println()
		fmt.Println("Warden scheduled task installed. Next steps:")
		fmt.Printf("  1. Edit %s to configure providers and routes\n", windowsConfigPath)
		fmt.Printf("  2. schtasks /Run /TN %q\n", windowsTaskName)
		return nil
	}
	if err := schtasks("/Run", "/TN", windowsTaskName); err != nil {
		return err
	}
	fmt.Println("  Scheduled task started")
	return nil
}

func finishWindowsUpdate(opts Options) error {
	if !ShouldStartAfterInstall(opts, "Restart Warden scheduled task now?") {
		fmt.Println()
		fmt.Println("Warden scheduled task updated. To apply:")
		fmt.Println("  1. Prefer an admin-triggered restart from Warden itself")
		fmt.Printf("  2. If the task is not running, start it with: schtasks /Run /TN %q\n", windowsTaskName)
		fmt.Printf("  3. Use schtasks /End /TN %q only as a non-graceful fallback\n", windowsTaskName)
		return nil
	}
	if windowsBinaryRunning() {
		fmt.Println("  Warden is already running. Apply the update with an admin-triggered restart.")
		fmt.Printf("  Use schtasks /End /TN %q only if graceful restart is unavailable.\n", windowsTaskName)
		return nil
	}
	if err := schtasks("/Run", "/TN", windowsTaskName); err != nil {
		return err
	}
	fmt.Println("  Scheduled task started")
	return nil
}

func installWindowsTask() error {
	return schtasks(
		"/Create",
		"/TN", windowsTaskName,
		"/SC", "ONSTART",
		"/RU", "SYSTEM",
		"/RL", "HIGHEST",
		"/TR", fmt.Sprintf(`cmd.exe /c ""%s""`, windowsScriptPath),
		"/F",
	)
}

func windowsTaskExists() bool {
	return exec.Command("schtasks", "/Query", "/TN", windowsTaskName).Run() == nil
}

func windowsBinaryRunning() bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq warden.exe", "/FO", "CSV", "/NH")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	text := strings.TrimSpace(string(output))
	if text == "" || strings.Contains(strings.ToLower(text), "no tasks are running") {
		return false
	}
	return tasklistHasOtherImageProcess(text, "warden.exe", os.Getpid())
}

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks %s: %w - %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
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
	return windowsConfigPath
}
