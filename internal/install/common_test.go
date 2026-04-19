package install

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestLaunchdPlistIncludesExpectedFields(t *testing.T) {
	plist := launchdPlist(
		"com.wweir.warden",
		"/usr/local/bin/warden",
		"/usr/local/etc/warden.yaml",
		"/usr/local/var/log/warden.log",
		"/usr/local/var/log/warden.err.log",
	)

	for _, want := range []string{
		"<string>com.wweir.warden</string>",
		"<string>/usr/local/bin/warden</string>",
		"<string>/usr/local/etc/warden.yaml</string>",
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
		`C:\ProgramData\Warden\warden.yaml`,
		`C:\ProgramData\Warden\logs\warden.log`,
	)

	for _, want := range []string{
		`set WARDEN_SUPERVISED=1`,
		`set WARDEN_RESTART_EXIT_CODE=75`,
		`"C:\Program Files\Warden\warden.exe" -c "C:\ProgramData\Warden\warden.yaml"`,
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
