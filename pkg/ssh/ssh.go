// Package ssh provides utilities for executing commands and reading files
// on remote hosts via the system ssh binary. It inherits the user's
// ~/.ssh/config, SSH agent, ProxyJump, certificates, etc.
package ssh

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Config holds SSH connection parameters.
// Fields mirror common ~/.ssh/config options; all optional fields are ignored
// when empty/zero, letting the system ssh config provide defaults.
type Config struct {
	Name         string // logical name (populated from config map key)
	Host         string // ~/.ssh/config Host entry or user@hostname
	Port         int
	User         string
	IdentityFile string
}

// BuildArgs constructs the ssh command-line arguments for the given Config
// and remote command. The returned slice does NOT include the "ssh" binary
// itself, so it can be passed to exec.Command("ssh", args...).
func BuildArgs(cfg *Config, command string, args ...string) []string {
	var sshArgs []string

	if cfg.Port != 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", cfg.Port))
	}
	if cfg.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", cfg.IdentityFile)
	}

	host := cfg.Host
	if cfg.User != "" && !strings.Contains(host, "@") {
		host = cfg.User + "@" + host
	}
	sshArgs = append(sshArgs, host)

	// append the remote command and its arguments
	sshArgs = append(sshArgs, command)
	sshArgs = append(sshArgs, args...)
	return sshArgs
}

// ReadFile reads a file from the remote host via "ssh host cat path".
func ReadFile(ctx context.Context, cfg *Config, path string) ([]byte, error) {
	args := BuildArgs(cfg, "cat", path)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ssh read file %s on %s: %w", path, cfg.Host, err)
	}
	return out, nil
}

// Command returns an *exec.Cmd that will execute the given command on the
// remote host via ssh. The caller is responsible for setting up pipes and
// starting the process.
func Command(cfg *Config, command string, args ...string) *exec.Cmd {
	sshArgs := BuildArgs(cfg, command, args...)
	return exec.Command("ssh", sshArgs...)
}

// CommandWithEnv returns an *exec.Cmd that will execute the given command on
// the remote host with the specified environment variables. Environment
// variables are prepended as "KEY=VALUE" assignments in a shell command,
// so they are set on the remote side.
func CommandWithEnv(cfg *Config, env map[string]string, command string, args ...string) *exec.Cmd {
	if len(env) == 0 {
		return Command(cfg, command, args...)
	}

	// build "VAR1=val1 VAR2=val2 command arg1 arg2" as a single shell string
	// env keys are not quoted (valid identifiers: [A-Za-z_][A-Za-z0-9_]*)
	var parts []string
	for k, v := range env {
		parts = append(parts, k+"="+shellQuote(v))
	}
	parts = append(parts, shellQuote(command))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	remoteCmd := strings.Join(parts, " ")

	var sshArgs []string
	if cfg.Port != 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", cfg.Port))
	}
	if cfg.IdentityFile != "" {
		sshArgs = append(sshArgs, "-i", cfg.IdentityFile)
	}

	host := cfg.Host
	if cfg.User != "" && !strings.Contains(host, "@") {
		host = cfg.User + "@" + host
	}
	sshArgs = append(sshArgs, host, remoteCmd)

	return exec.Command("ssh", sshArgs...)
}

// shellQuote wraps a string in single quotes, escaping any embedded single
// quotes. This is safe for POSIX shells.
func shellQuote(s string) string {
	// replace ' with '\'' (end quote, escaped quote, start quote)
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
