package install

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wweir/warden/config"
)

func TestShouldStartAfterInstall(t *testing.T) {
	falseValue := false
	if ShouldStartAfterInstall(Options{StartAfterInstall: &falseValue}, "ignored") {
		t.Fatal("ShouldStartAfterInstall() = true, want false")
	}

	called := false
	got := ShouldStartAfterInstall(Options{
		Confirm: func(label string) bool {
			called = true
			return label == "start?"
		},
	}, "start?")
	if !called || !got {
		t.Fatalf("ShouldStartAfterInstall() = %v, called=%v, want true/true", got, called)
	}
}

func TestShouldExposeManagedService(t *testing.T) {
	trueValue := true
	if !ShouldExposeManagedService(Options{ExposeExternally: &trueValue}) {
		t.Fatal("ShouldExposeManagedService() = false, want true")
	}

	called := false
	got := ShouldExposeManagedService(Options{
		Confirm: func(label string) bool {
			called = true
			return strings.Contains(label, "Expose Warden on all network interfaces?")
		},
	})
	if !called || !got {
		t.Fatalf("ShouldExposeManagedService() = %v, called=%v, want true/true", got, called)
	}
}

func TestEnsureManagedBootstrapConfigWritesLocalOnlyManagedBootstrapConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	adminPassword := "local-secret"

	var created bool
	output := captureStdout(t, func() {
		var err error
		created, err = ensureManagedBootstrapConfig(configPath, Options{AdminPassword: &adminPassword})
		if err != nil {
			t.Fatalf("ensureManagedBootstrapConfig() error = %v", err)
		}
	})
	if !created {
		t.Fatal("ensureManagedBootstrapConfig() = false, want true")
	}
	assertManagedConfigMode(t, configPath)
	for _, want := range []string{
		"Created default config: " + configPath,
		"Bootstrap config listens on localhost only: http://localhost:9832/_admin/",
		`Admin UI username is "admin"; use the password entered during installation`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `addr = "127.0.0.1:9832"`) {
		t.Fatalf("config missing addr, got:\n%s", text)
	}
	for _, want := range []string{
		`admin_password = "` + config.EncodeSecret(adminPassword) + `"`,
		`Admin UI: http://localhost:9832/_admin/`,
		`Username is "admin"; password was set during installation.`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("managed bootstrap config missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{
		"provider:",
		"route:",
		"api.openai.com",
		"copilot:",
	} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("managed bootstrap config contains %q:\n%s", unwanted, text)
		}
	}
}

func TestEnsureManagedBootstrapConfigKeepsNonInteractiveLocalDefaultAdminPassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")

	var created bool
	output := captureStdout(t, func() {
		var err error
		created, err = ensureManagedBootstrapConfig(configPath, Options{})
		if err != nil {
			t.Fatalf("ensureManagedBootstrapConfig() error = %v", err)
		}
	})
	if !created {
		t.Fatal("ensureManagedBootstrapConfig() = false, want true")
	}
	assertManagedConfigMode(t, configPath)
	if !strings.Contains(output, `Admin UI username is "admin"; update admin_password before exposing Warden`) {
		t.Fatalf("output missing non-interactive default guidance:\n%s", output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `admin_password = "`+config.EncodeSecret(defaultAdminPassword)+`"`) {
		t.Fatalf("config missing encoded default admin password, got:\n%s", text)
	}
}

func TestEnsureManagedBootstrapConfigWritesExternallyExposedBootstrapConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	expose := true

	var created bool
	output := captureStdout(t, func() {
		var err error
		created, err = ensureManagedBootstrapConfig(configPath, Options{ExposeExternally: &expose})
		if err != nil {
			t.Fatalf("ensureManagedBootstrapConfig() error = %v", err)
		}
	})
	if !created {
		t.Fatal("ensureManagedBootstrapConfig() = false, want true")
	}
	assertManagedConfigMode(t, configPath)
	for _, want := range []string{
		"Created default config: " + configPath,
		"Bootstrap config listens on all network interfaces via port 9832",
		`Admin UI is disabled until admin_password is set in the config`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `addr = ":9832"`) {
		t.Fatalf("config missing external addr, got:\n%s", text)
	}
	for _, want := range []string{
		`Admin UI stays disabled in the external non-interactive bootstrap config.`,
		`# admin_password = "replace-with-a-strong-secret"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, `admin_password = "admin"`) {
		t.Fatalf("external bootstrap config should not contain default admin password, got:\n%s", text)
	}
}

func TestEnsureManagedBootstrapConfigPromptsForExternalAdminPassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	expose := true
	var promptLabel string

	var created bool
	output := captureStdout(t, func() {
		var err error
		created, err = ensureManagedBootstrapConfig(configPath, Options{
			ExposeExternally: &expose,
			AdminPasswordPrompt: func(label string) (string, bool) {
				promptLabel = label
				return "external-secret", true
			},
		})
		if err != nil {
			t.Fatalf("ensureManagedBootstrapConfig() error = %v", err)
		}
	})
	if !created {
		t.Fatal("ensureManagedBootstrapConfig() = false, want true")
	}
	assertManagedConfigMode(t, configPath)
	if promptLabel != "Enter admin password for Warden Admin UI" {
		t.Fatalf("prompt label = %q", promptLabel)
	}
	for _, want := range []string{
		`Admin UI username is "admin"; use the password entered during installation`,
		"Admin UI is reachable on exposed interfaces; keep this password strong and private",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`addr = ":9832"`,
		`Admin UI: http://<host>:9832/_admin/`,
		`admin_password = "` + config.EncodeSecret("external-secret") + `"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q, got:\n%s", want, text)
		}
	}
}

func TestEnsureManagedBootstrapConfigKeepsExistingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")
	original := []byte("addr = \":9000\"\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	var created bool
	output := captureStdout(t, func() {
		var err error
		created, err = ensureManagedBootstrapConfig(configPath, Options{})
		if err != nil {
			t.Fatalf("ensureManagedBootstrapConfig() error = %v", err)
		}
	})
	if created {
		t.Fatal("ensureManagedBootstrapConfig() = true, want false")
	}
	if output != "" {
		t.Fatalf("ensureManagedBootstrapConfig() output = %q, want empty", output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != string(original) {
		t.Fatalf("config was overwritten, got %q want %q", string(data), string(original))
	}
}

func TestEnsureManagedBootstrapConfigRejectsEmptyPromptedAdminPassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "warden.toml")

	created, err := ensureManagedBootstrapConfig(configPath, Options{
		AdminPasswordPrompt: func(string) (string, bool) {
			return "", true
		},
	})
	if err == nil {
		t.Fatal("ensureManagedBootstrapConfig() error = nil, want error")
	}
	if created {
		t.Fatal("ensureManagedBootstrapConfig() created config with empty admin password")
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config file stat error = %v, want not exist", statErr)
	}
}

func TestValidateManagedAdminPasswordRejectsWeakPasswords(t *testing.T) {
	if err := ValidateManagedAdminPassword("a"); err == nil {
		t.Fatal("ValidateManagedAdminPassword() error = nil, want weak password rejection")
	}
	if err := ValidateManagedAdminPassword("short-pass"); err == nil {
		t.Fatal("ValidateManagedAdminPassword() error = nil, want minimum length rejection")
	}
	if err := ValidateManagedAdminPassword("strong-passphrase"); err != nil {
		t.Fatalf("ValidateManagedAdminPassword() error = %v, want nil", err)
	}
}

func assertManagedConfigMode(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != managedConfigMode {
		t.Fatalf("config mode = %o, want %o", got, managedConfigMode)
	}
}

func TestLaunchdPlistIncludesExpectedFields(t *testing.T) {
	plist := launchdPlist(
		"com.wweir.warden",
		"/usr/local/bin/warden",
		"/usr/local/etc/warden/warden.toml",
		"/usr/local/var/log/warden.log",
		"/usr/local/var/log/warden.err.log",
	)

	for _, want := range []string{
		"<string>com.wweir.warden</string>",
		"<string>/usr/local/bin/warden</string>",
		"<string>/usr/local/etc/warden/warden.toml</string>",
		"<string>/usr/local/var/log/warden.log</string>",
		"<string>/usr/local/var/log/warden.err.log</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q", want)
		}
	}
}

func TestWindowsTaskScriptIncludesBinaryConfigAndLog(t *testing.T) {
	script := windowsTaskScript(
		`C:\Program Files\Warden\warden.exe`,
		`C:\ProgramData\Warden\warden.toml`,
		`C:\ProgramData\Warden\logs\warden.log`,
	)

	for _, want := range []string{
		`set WARDEN_SUPERVISED=1`,
		`set WARDEN_RESTART_EXIT_CODE=75`,
		`"C:\Program Files\Warden\warden.exe" -c "C:\ProgramData\Warden\warden.toml"`,
		`>> "C:\ProgramData\Warden\logs\warden.log" 2>&1`,
		`if "%EXIT_CODE%"=="75" goto restart`,
		`if "%EXIT_CODE%"=="0" exit /b 0`,
		`goto restart`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q", want)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(data)
}

func TestTasklistHasOtherImageProcess(t *testing.T) {
	output := strings.Join([]string{
		`"warden.exe","1234","Console","1","12,000 K"`,
		`"warden.exe","5678","Console","1","13,000 K"`,
		`"other.exe","9999","Console","1","10,000 K"`,
	}, "\n")

	if !tasklistHasOtherImageProcess(output, "warden.exe", 1234) {
		t.Fatal("tasklistHasOtherImageProcess() = false, want true when another PID matches")
	}
	if tasklistHasOtherImageProcess(output, "warden.exe", 5678) != true {
		t.Fatal("tasklistHasOtherImageProcess() should still detect the other matching PID")
	}
	if tasklistHasOtherImageProcess(`"warden.exe","1234","Console","1","12,000 K"`, "warden.exe", 1234) {
		t.Fatal("tasklistHasOtherImageProcess() = true, want false when only current PID matches")
	}
}

func TestFinishLinuxUpdateStartsAndEnablesInactiveService(t *testing.T) {
	original := runSystemctl
	defer func() {
		runSystemctl = original
	}()

	var calls [][]string
	runSystemctl = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	start := true
	if err := finishLinuxUpdate(Options{StartAfterInstall: &start}, false, false); err != nil {
		t.Fatalf("finishLinuxUpdate() error = %v", err)
	}

	want := [][]string{
		{"enable", linuxServiceName},
		{"start", linuxServiceName},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("finishLinuxUpdate() calls = %v, want %v", calls, want)
	}
}

func TestFinishLinuxUpdateRestartsActiveServiceWithoutEnable(t *testing.T) {
	original := runSystemctl
	defer func() {
		runSystemctl = original
	}()

	var calls [][]string
	runSystemctl = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	start := true
	if err := finishLinuxUpdate(Options{StartAfterInstall: &start}, true, true); err != nil {
		t.Fatalf("finishLinuxUpdate() error = %v", err)
	}

	want := [][]string{
		{"restart", linuxServiceName},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("finishLinuxUpdate() calls = %v, want %v", calls, want)
	}
}

func TestFinishLinuxUpdatePropagatesEnableError(t *testing.T) {
	original := runSystemctl
	defer func() {
		runSystemctl = original
	}()

	wantErr := errors.New("enable failed")
	runSystemctl = func(args ...string) error {
		if len(args) > 0 && args[0] == "enable" {
			return wantErr
		}
		return nil
	}

	start := true
	err := finishLinuxUpdate(Options{StartAfterInstall: &start}, false, false)
	if !errors.Is(err, wantErr) {
		t.Fatalf("finishLinuxUpdate() error = %v, want %v", err, wantErr)
	}
}
