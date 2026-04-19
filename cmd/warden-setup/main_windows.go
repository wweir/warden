//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/wweir/warden/internal/install"
	"github.com/wweir/warden/internal/setupbundle"
)

var Version, BuildTime string

func main() {
	if err := run(); err != nil {
		showError(err.Error())
		os.Exit(1)
	}
}

func run() error {
	if !hasArg("--elevated") {
		if err := relaunchElevated(); err != nil {
			return fmt.Errorf("request administrator privileges: %w", err)
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get setup executable path: %w", err)
	}
	payload, err := setupbundle.ExtractFromFile(executable)
	if err != nil {
		return fmt.Errorf("extract embedded installer payload: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "warden-setup-*")
	if err != nil {
		return fmt.Errorf("create setup temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	runtimePath, err := extractRuntime(tempDir, payload)
	if err != nil {
		return err
	}

	cmd := exec.Command(runtimePath, "-i", "--non-interactive", "--start")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("run embedded installer: %w", err)
		}
		return fmt.Errorf("run embedded installer: %w\n%s", err, trimmed)
	}

	showInfo(fmt.Sprintf("Warden 安装完成。\n\n运行时：C:\\Program Files\\Warden\\warden.exe\n配置：%s", install.ManagedConfigPath()))
	return nil
}

func hasArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if arg == target {
			return true
		}
	}
	return false
}

func relaunchElevated() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get setup executable path: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	args := make([]string, 0, len(os.Args))
	for _, arg := range append(os.Args[1:], "--elevated") {
		args = append(args, syscall.EscapeArg(arg))
	}
	return shellExecute("runas", executable, strings.Join(args, " "), cwd)
}

func extractRuntime(destDir string, payload []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return "", fmt.Errorf("open installer payload zip: %w", err)
	}

	destRoot := filepath.Clean(destDir) + string(os.PathSeparator)
	runtimePath := ""
	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." || name == "" || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return "", fmt.Errorf("installer payload contains invalid path %q", file.Name)
		}
		targetPath := filepath.Join(destDir, name)
		if !strings.HasPrefix(targetPath, destRoot) {
			return "", fmt.Errorf("installer payload escapes target dir: %q", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", fmt.Errorf("create payload dir %s: %w", targetPath, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", fmt.Errorf("create parent dir for %s: %w", targetPath, err)
		}

		rc, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open payload file %s: %w", file.Name, err)
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			rc.Close()
			return "", fmt.Errorf("create extracted file %s: %w", targetPath, err)
		}
		if _, err := io.Copy(dst, rc); err != nil {
			dst.Close()
			rc.Close()
			return "", fmt.Errorf("write extracted file %s: %w", targetPath, err)
		}
		if err := dst.Close(); err != nil {
			rc.Close()
			return "", fmt.Errorf("close extracted file %s: %w", targetPath, err)
		}
		if err := rc.Close(); err != nil {
			return "", fmt.Errorf("close payload file %s: %w", file.Name, err)
		}
		if strings.EqualFold(filepath.Base(targetPath), "warden.exe") {
			runtimePath = targetPath
		}
	}
	if runtimePath == "" {
		return "", fmt.Errorf("installer payload missing warden.exe")
	}
	return runtimePath, nil
}

func shellExecute(verb, executable, parameters, dir string) error {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")
	result, _, callErr := proc.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(verb))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(executable))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(parameters))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(dir))),
		uintptr(1),
	)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW returned %d: %v", result, callErr)
	}
	return nil
}

func showError(message string) {
	showMessageBox("Warden Setup Error", message, 0x10)
}

func showInfo(message string) {
	showMessageBox("Warden Setup", message, 0x40)
}

func showMessageBox(title, message string, icon uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	proc.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		icon,
	)
}
