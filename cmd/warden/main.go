package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/yaml"
	_ "github.com/sower-proxy/feconf/reader/file"
	_ "github.com/sower-proxy/feconf/reader/http"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/gateway"
	"github.com/wweir/warden/internal/install"
)

var Version, BuildTime string

// pidFilePath returns a user-specific pid file path.
func pidFilePath() string {
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "warden.pid")
	}
	return fmt.Sprintf("/tmp/warden-%d.pid", os.Getuid())
}

var pidFile = pidFilePath()

var configCandidates = []string{
	"warden.yaml", "warden.yml",
	"config/warden.yaml", "config/warden.yml",
	"/etc/warden.yaml", "/etc/warden.yml",
}

type app struct {
	cfg        *config.ConfigStruct
	configPath string
	gateway    *gateway.Gateway
	restartCh  chan struct{}
}

func newApp(cfg *config.ConfigStruct, configPath string) *app {
	configHash := ""
	if configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			configHash = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}

	gw := gateway.NewGateway(cfg, configPath, configHash)

	return &app{
		cfg:        cfg,
		configPath: configPath,
		gateway:    gw,
		restartCh:  make(chan struct{}, 1),
	}
}

// ServeHTTP implements http.Handler.
func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.gateway.ServeHTTP(w, r)
}

// restart signals the main loop to perform an in-place restart via syscall.Exec.
func (a *app) restart() error {
	select {
	case a.restartCh <- struct{}{}:
	default: // restart already pending
	}
	return nil
}

func (a *app) run() (err error) {
	defer func() { deferlog.DebugError(err, "app run") }()

	// write pid file for reload signaling
	if err := writePidFile(); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer removePidFile()

	a.gateway.SetReloadFn(a.restart)

	listener, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.cfg.Addr, err)
	}

	server := &http.Server{
		Addr:    a.cfg.Addr,
		Handler: a,
	}

	go func() {
		slog.Info("Gateway is listening", "addr", a.cfg.Addr)
		if err := server.Serve(listener); err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			os.Exit(1)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	var doRestart bool
	select {
	case sig := <-stopChan:
		if sig == syscall.SIGHUP {
			slog.Info("Received SIGHUP, restarting...")
			doRestart = true
		} else {
			slog.Info("Shutting down...", "signal", sig)
		}
	case <-a.restartCh:
		slog.Info("Restarting with new config...")
		doRestart = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Warn("Server shutdown timeout, forcing restart", "error", err)
	}

	a.gateway.Close()

	if doRestart {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable: %w", err)
		}
		slog.Info("Exec restart", "executable", executable, "args", os.Args)
		if err := syscall.Exec(executable, os.Args, os.Environ()); err != nil {
			return fmt.Errorf("exec restart: %w", err)
		}
	}

	slog.Info("Warden shutdown complete")
	return nil
}

func main() {
	// parse command-line flags
	installService := flag.Bool("i", false, "install as systemd service")
	reload := flag.Bool("r", false, "reload running service (send SIGHUP)")

	// load configuration
	cfg, err := feconf.New[config.ConfigStruct]("c", configCandidates...).Parse()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("Failed to validate config", "error", err)
		os.Exit(1)
	}

	// detect config file path
	configPath := detectConfigPath()

	// configure logging output
	fi, _ := os.Stdout.Stat()
	isTerminal := (fi.Mode() & os.ModeCharDevice) != 0
	deferlog.SetDefault(slog.New(tint.NewHandler(os.Stderr,
		&tint.Options{Level: slog.LevelDebug, AddSource: true, NoColor: !isTerminal})))

	slog.Info("Starting Warden AI Gateway",
		"version", Version,
		"build_time", BuildTime,
		"config", cfg)

	// mode dispatch
	if *installService {
		if err := install.InstallService(stdinConfirm); err != nil {
			slog.Error("Install service error", "error", err)
			os.Exit(1)
		}
		return
	}

	if *reload {
		if err := sendReloadSignal(); err != nil {
			slog.Error("Failed to send reload signal", "error", err)
			os.Exit(1)
		}
		fmt.Println("Reload signal sent successfully")
		return
	}

	application := newApp(cfg, configPath)
	if err := application.run(); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

// detectConfigPath finds the first existing config file from the candidate list.
// Returns empty string if -c flag was used (feconf handles it) or no candidate exists.
func detectConfigPath() string {
	// check if -c flag was explicitly set
	var cFlag string
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "c" {
			cFlag = f.Value.String()
		}
	})
	if cFlag != "" {
		return cFlag
	}

	for _, candidate := range configCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// stdinConfirm prompts the user for yes/no confirmation via stdin.
func stdinConfirm(label string) bool {
	fmt.Printf("%s [y/N]: ", label)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

// writePidFile writes the current process PID to the pid file.
// If a pid file already exists, it checks whether that process is still running:
// - If running: returns an error suggesting to use the reload feature
// - If not running: removes the stale pid file and creates a new one
func writePidFile() error {
	// check for existing pid file
	if data, err := os.ReadFile(pidFile); err == nil {
		var oldPid int
		if _, err := fmt.Sscanf(string(data), "%d", &oldPid); err == nil {
			// check if the process is still running
			if process, err := os.FindProcess(oldPid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					// process exists, suggest reload
					return fmt.Errorf("warden is already running (pid %d), use -r to reload", oldPid)
				}
			}
			// process not running, remove stale pid file
			os.Remove(pidFile)
		}
	}

	pid := os.Getpid()
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// removePidFile removes the pid file.
func removePidFile() {
	os.Remove(pidFile)
}

// readPidFile reads the PID from the pid file.
func readPidFile() (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}
	return pid, nil
}

// sendReloadSignal sends SIGHUP to the process specified in the pid file.
func sendReloadSignal() error {
	pid, err := readPidFile()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("warden is not running (no pid file at %s)", pidFile)
		}
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("send SIGHUP to process %d: %w", pid, err)
	}
	return nil
}
