package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/wweir/warden/pkg/provider"
)

const (
	defaultAPIKeyCommandTimeout = 5 * time.Second
	defaultAPIKeyCommandTTL     = 5 * time.Minute
	apiKeyCommandOutputLimit    = 64 * 1024
)

var errAPIKeyCommandOutputTooLarge = errors.New("api key command output exceeds limit")

// ResolveAPIKey returns the effective provider credential.
func (b *ProviderConfig) ResolveAPIKey(ctx context.Context) (string, error) {
	if b == nil {
		return "", nil
	}
	if b.APIKey.Value() != "" {
		return b.APIKey.Value(), nil
	}
	if strings.TrimSpace(b.APIKeyCommand) != "" {
		return b.resolveAPIKeyCommand(ctx)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if p := provider.Get(b.Format); p != nil {
		token, err := p.GetAccessToken(ctx, b.ConfigDir)
		if err != nil {
			return "", err
		}
		return token, nil
	}
	return "", nil
}

func (b *ProviderConfig) resolveAPIKeyCommand(ctx context.Context) (string, error) {
	command := strings.TrimSpace(b.APIKeyCommand)
	if command == "" {
		return "", nil
	}

	ttl := b.apiKeyCommandTTL()
	if ttl > 0 {
		b.apiKeyCommandMu.Lock()
		if b.apiKeyCommandCache.Command == command && b.apiKeyCommandCache.Token != "" && time.Now().Before(b.apiKeyCommandCache.ExpiresAt) {
			token := b.apiKeyCommandCache.Token
			b.apiKeyCommandMu.Unlock()
			return token, nil
		}
		b.apiKeyCommandMu.Unlock()
	}

	token, err := runAPIKeyCommand(ctx, command, b.apiKeyCommandTimeout())
	if err != nil {
		return "", err
	}

	if ttl > 0 {
		b.apiKeyCommandMu.Lock()
		b.apiKeyCommandCache.Command = command
		b.apiKeyCommandCache.Token = token
		b.apiKeyCommandCache.ExpiresAt = time.Now().Add(ttl)
		b.apiKeyCommandMu.Unlock()
	}
	return token, nil
}

func (b *ProviderConfig) clearAPIKeyCommandCache() {
	if b == nil {
		return
	}
	b.apiKeyCommandMu.Lock()
	b.apiKeyCommandCache = providerAPIKeyCommandCache{}
	b.apiKeyCommandMu.Unlock()
}

func (b *ProviderConfig) apiKeyCommandTimeout() time.Duration {
	if timeout := strings.TrimSpace(b.APIKeyCommandTimeout); timeout != "" {
		if parsed, err := time.ParseDuration(timeout); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultAPIKeyCommandTimeout
}

func (b *ProviderConfig) apiKeyCommandTTL() time.Duration {
	if ttl := strings.TrimSpace(b.APIKeyCommandTTL); ttl != "" {
		if parsed, err := time.ParseDuration(ttl); err == nil {
			return parsed
		}
	}
	return defaultAPIKeyCommandTTL
}

func runAPIKeyCommand(ctx context.Context, command string, timeout time.Duration) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := shellCommand(ctx, command)
	var stdout limitedBuffer
	var stderr limitedBuffer
	stdout.limit = apiKeyCommandOutputLimit
	stderr.limit = apiKeyCommandOutputLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("api key command timed out or was cancelled: %w", ctx.Err())
	}
	if errors.Is(err, errAPIKeyCommandOutputTooLarge) || stdout.TooLarge() || stderr.TooLarge() {
		return "", errAPIKeyCommandOutputTooLarge
	}
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return "", fmt.Errorf("api key command failed (exit code %d, stderr: %s): %w", exitCode, stderrText, err)
		}
		return "", fmt.Errorf("api key command failed (exit code %d): %w", exitCode, err)
	}

	raw := stdout.String()
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", fmt.Errorf("api key command returned empty token")
	}
	if strings.Contains(token, "\n") || strings.Contains(token, "\r") {
		return "", fmt.Errorf("api key command returned multiple lines")
	}
	return token, nil
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

type limitedBuffer struct {
	limit int
	buf   bytes.Buffer
	large bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.large = true
		return 0, errAPIKeyCommandOutputTooLarge
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.large = true
		return remaining, errAPIKeyCommandOutputTooLarge
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	data, _ := io.ReadAll(bytes.NewReader(b.buf.Bytes()))
	return string(data)
}

func (b *limitedBuffer) TooLarge() bool {
	return b.large
}
