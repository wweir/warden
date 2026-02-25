package ssh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		command string
		args    []string
		want    []string
	}{
		{
			name:    "host only",
			cfg:     &Config{Host: "dev-server"},
			command: "cat",
			args:    []string{"/tmp/file"},
			want:    []string{"dev-server", "cat", "/tmp/file"},
		},
		{
			name:    "with port",
			cfg:     &Config{Host: "dev-server", Port: 2222},
			command: "ls",
			want:    []string{"-p", "2222", "dev-server", "ls"},
		},
		{
			name:    "with user",
			cfg:     &Config{Host: "example.com", User: "deploy"},
			command: "whoami",
			want:    []string{"deploy@example.com", "whoami"},
		},
		{
			name:    "user already in host",
			cfg:     &Config{Host: "root@server", User: "ignored"},
			command: "whoami",
			want:    []string{"root@server", "whoami"},
		},
		{
			name:    "with identity file",
			cfg:     &Config{Host: "server", IdentityFile: "/home/user/.ssh/id_ed25519"},
			command: "echo",
			args:    []string{"hello"},
			want:    []string{"-i", "/home/user/.ssh/id_ed25519", "server", "echo", "hello"},
		},
		{
			name:    "all options",
			cfg:     &Config{Host: "server", Port: 22, User: "admin", IdentityFile: "/key"},
			command: "cat",
			args:    []string{"/etc/hosts"},
			want:    []string{"-p", "22", "-i", "/key", "admin@server", "cat", "/etc/hosts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildArgs(tt.cfg, tt.command, tt.args...)
			if len(got) != len(tt.want) {
				t.Fatalf("BuildArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("BuildArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCommandWithEnv(t *testing.T) {
	cfg := &Config{Host: "dev-server"}
	env := map[string]string{
		"FOO": "bar",
	}

	cmd := CommandWithEnv(cfg, env, "npx", "-y", "some-pkg")

	if cmd.Path == "" {
		t.Fatal("CommandWithEnv() returned cmd with empty Path")
	}

	// the last arg should be the combined remote command string
	args := cmd.Args
	// args[0] is "ssh", args[1] is host, args[2] is the remote command
	remoteCmd := args[len(args)-1]

	if !strings.Contains(remoteCmd, "FOO='bar'") {
		t.Errorf("remote command should contain env var, got: %s", remoteCmd)
	}
	if !strings.Contains(remoteCmd, "'npx'") {
		t.Errorf("remote command should contain quoted command, got: %s", remoteCmd)
	}
	if !strings.Contains(remoteCmd, "'-y'") {
		t.Errorf("remote command should contain quoted args, got: %s", remoteCmd)
	}
}

func TestCommandWithEnv_empty(t *testing.T) {
	cfg := &Config{Host: "dev-server", Port: 2222}

	cmd := CommandWithEnv(cfg, nil, "cat", "/tmp/file")
	args := cmd.Args

	// should fall back to Command() behavior: ssh -p 2222 dev-server cat /tmp/file
	want := []string{"ssh", "-p", "2222", "dev-server", "cat", "/tmp/file"}
	if len(args) != len(want) {
		t.Fatalf("CommandWithEnv(nil env) args = %v, want %v", args, want)
	}
	for i := range args {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// sshAvailable checks if ssh localhost is reachable (for integration tests).
func sshAvailable(t *testing.T) {
	t.Helper()
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3", "localhost", "true")
	if err := cmd.Run(); err != nil {
		t.Skip("ssh localhost not available, skipping integration test")
	}
}

func TestIntegration_ReadFile(t *testing.T) {
	sshAvailable(t)

	// create a temp file with known content
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello from integration test\nsecond line\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg := &Config{Host: "localhost"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := ReadFile(ctx, cfg, path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("ReadFile got %q, want %q", string(data), content)
	}
}

func TestIntegration_ReadFile_NotFound(t *testing.T) {
	sshAvailable(t)

	cfg := &Config{Host: "localhost"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := ReadFile(ctx, cfg, "/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestIntegration_Command(t *testing.T) {
	sshAvailable(t)

	cfg := &Config{Host: "localhost"}
	cmd := Command(cfg, "echo", "hello world")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Command echo: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "hello world" {
		t.Errorf("Command echo got %q, want %q", got, "hello world")
	}
}

func TestIntegration_CommandWithEnv(t *testing.T) {
	sshAvailable(t)

	cfg := &Config{Host: "localhost"}
	env := map[string]string{
		"TEST_VAR_1": "value1",
		"TEST_VAR_2": "hello world",
	}

	// use env/printenv to verify env vars are set (no shell expansion needed)
	cmd := CommandWithEnv(cfg, env, "printenv", "TEST_VAR_1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("CommandWithEnv: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "value1" {
		t.Errorf("CommandWithEnv got %q, want %q", got, "value1")
	}
}

func TestIntegration_CommandWithEnv_SpecialChars(t *testing.T) {
	sshAvailable(t)

	cfg := &Config{Host: "localhost"}
	env := map[string]string{
		"MY_VAR": "it's a \"test\" with $pecial chars",
	}

	cmd := CommandWithEnv(cfg, env, "printenv", "MY_VAR")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("CommandWithEnv special chars: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "it's a \"test\" with $pecial chars"
	if got != want {
		t.Errorf("CommandWithEnv special chars got %q, want %q", got, want)
	}
}

func TestIntegration_Command_StdinStdout(t *testing.T) {
	sshAvailable(t)

	// test that stdin/stdout pipes work (critical for MCP JSON-RPC)
	cfg := &Config{Host: "localhost"}
	cmd := Command(cfg, "cat")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// write to stdin
	msg := "test message\n"
	if _, err := stdin.Write([]byte(msg)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	stdin.Close()

	// read from stdout
	buf := make([]byte, 1024)
	n, err := stdout.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := string(buf[:n])
	if got != msg {
		t.Errorf("stdin/stdout pipe got %q, want %q", got, msg)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}
