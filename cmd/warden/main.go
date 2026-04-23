package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
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

const managedRestartExitCode = 75

var pidFile = pidFilePath()

var configCandidates = []string{
	"warden.yaml", "warden.yml",
	"config/warden.yaml", "config/warden.yml",
	"/etc/warden.yaml", "/etc/warden.yml",
}

type processExitError struct {
	code int
}

func (e *processExitError) Error() string {
	return fmt.Sprintf("process exit requested with code %d", e.code)
}

type modeFlags struct {
	install           bool
	reload            bool
	nonInteractive    bool
	startAfterInstall *bool
	exposeExternally  *bool
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

// restart signals the main loop to restart the current process.
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
	pidFileOwned := true
	defer func() {
		if pidFileOwned {
			removePidFile()
		}
	}()

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
	notifyStopSignals(stopChan)

	var doRestart bool
	select {
	case sig := <-stopChan:
		if isRestartSignal(sig) {
			slog.Info("Received restart signal", "signal", sig)
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
		if restartNeedsPIDFileRemoval() {
			removePidFile()
			pidFileOwned = false
		}
		if err := restartCurrentProcess(); err != nil {
			return fmt.Errorf("restart current process: %w", err)
		}
		return nil
	}

	slog.Info("Warden shutdown complete")
	return nil
}

func main() {
	// parse command-line flags
	flag.Bool("i", false, "install as managed service")
	flag.Bool("r", false, "reload running service")
	flag.Bool("non-interactive", false, "install without interactive prompts")
	flag.Bool("start", false, "start service after install")
	flag.Bool("no-start", false, "do not start service after install")
	flag.Bool("expose", false, "for managed install, bind bootstrap config to all network interfaces")
	flag.Bool("local-only", false, "for managed install, bind bootstrap config to localhost only")

	configureLogging()

	mode := parseModeFlags(os.Args[1:])

	if mode.install {
		if err := validateExistingManagedConfig(); err != nil {
			slog.Error("Failed to validate managed config", "error", err)
			os.Exit(1)
		}
		if err := install.InstallService(buildInstallOptions(mode)); err != nil {
			slog.Error("Install service error", "error", err)
			os.Exit(1)
		}
		return
	}

	if mode.reload {
		if err := sendReloadSignal(); err != nil {
			slog.Error("Failed to send reload signal", "error", err)
			os.Exit(1)
		}
		fmt.Println("Reload signal sent successfully")
		return
	}

	// load configuration
	cfg, configPath, err := loadConfig()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("Failed to validate config", "error", err)
		os.Exit(1)
	}

	slog.Info("Starting Warden AI Gateway",
		"version", Version,
		"build_time", BuildTime,
		"config", cfg)

	application := newApp(cfg, configPath)
	if err := application.run(); err != nil {
		if code, ok := exitCodeFromError(err); ok {
			os.Exit(code)
		}
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (*config.ConfigStruct, string, error) {
	conf := feconf.New[config.ConfigStruct]("c", configCandidates...)
	conf.ParserConf = buildConfigParserConfig()
	cfg, err := safeParseConfig(conf)
	configPath := detectConfigPath()
	return cfg, configPath, err
}

func loadConfigFromPath(configPath string) (*config.ConfigStruct, error) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	defer func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	}()

	programName := "warden"
	if len(oldArgs) > 0 && oldArgs[0] != "" {
		programName = oldArgs[0]
	}
	os.Args = []string{programName}
	flag.CommandLine = flag.NewFlagSet(programName, flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)

	conf := feconf.New[config.ConfigStruct]("c", configPath)
	conf.ParserConf = buildConfigParserConfig()
	return safeParseConfig(conf)
}

func configureLogging() {
	fi, _ := os.Stdout.Stat()
	isTerminal := (fi.Mode() & os.ModeCharDevice) != 0
	deferlog.SetDefault(slog.New(tint.NewHandler(os.Stderr,
		&tint.Options{Level: slog.LevelDebug, AddSource: true, NoColor: !isTerminal})))
}

func parseModeFlags(args []string) modeFlags {
	var flags modeFlags
	for _, arg := range args {
		switch arg {
		case "-i", "-i=true":
			flags.install = true
		case "-r", "-r=true":
			flags.reload = true
		case "-i=false":
			flags.install = false
		case "-r=false":
			flags.reload = false
		case "--non-interactive", "--non-interactive=true":
			flags.nonInteractive = true
		case "--non-interactive=false":
			flags.nonInteractive = false
		case "--start", "--start=true":
			flags.startAfterInstall = boolPtr(true)
		case "--start=false", "--no-start", "--no-start=true":
			flags.startAfterInstall = boolPtr(false)
		case "--no-start=false":
			flags.startAfterInstall = boolPtr(true)
		case "--expose", "--expose=true":
			flags.exposeExternally = boolPtr(true)
		case "--expose=false", "--local-only", "--local-only=true":
			flags.exposeExternally = boolPtr(false)
		case "--local-only=false":
			flags.exposeExternally = boolPtr(true)
		}
	}
	return flags
}

func buildInstallOptions(mode modeFlags) install.Options {
	opts := install.Options{
		StartAfterInstall: mode.startAfterInstall,
		ExposeExternally:  mode.exposeExternally,
	}
	if !mode.nonInteractive {
		opts.Confirm = stdinConfirm
	}
	return opts
}

func boolPtr(v bool) *bool {
	return &v
}

func validateExistingManagedConfig() error {
	return validateConfigPath(install.ManagedConfigPath())
}

func validateConfigPath(configPath string) error {
	if configPath == "" {
		return nil
	}
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat config %s: %w", configPath, err)
	}

	cfg, err := loadConfigFromPath(configPath)
	if err != nil {
		return fmt.Errorf("load config %s: %w", configPath, err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config %s: %w", configPath, err)
	}
	return nil
}

func exitCodeFromError(err error) (int, bool) {
	var exitErr *processExitError
	if errors.As(err, &exitErr) {
		return exitErr.code, true
	}
	return 0, false
}

func safeParseConfig(conf *feconf.ConfOpt[config.ConfigStruct]) (cfg *config.ConfigStruct, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("feconf parse panic: %v", r)
		}
	}()
	return conf.Parse()
}

func buildConfigParserConfig() mapstructure.DecoderConfig {
	cfg := feconf.DefaultParserConfig
	cfg.DecodeHook = mapstructure.ComposeDecodeHookFunc(
		hookFuncNilDefault(),
		feconf.HookFuncEnvRender(),
		feconf.HookFuncStringToBool(),
		feconf.HookFuncStringToSlogLevel(),
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructure.StringToBasicTypeHookFunc(),
		config.HookFuncStringToSecretString(),
	)
	return cfg
}

func hookFuncNilDefault() mapstructure.DecodeHookFuncType {
	return func(_ reflect.Type, t reflect.Type, data any) (any, error) {
		if data != nil {
			return data, nil
		}
		switch t.Kind() {
		case reflect.String:
			return "", nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return int64(0), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return uint64(0), nil
		case reflect.Float32, reflect.Float64:
			return float64(0), nil
		case reflect.Bool:
			return false, nil
		case reflect.Struct:
			return reflect.Zero(t).Interface(), nil
		default:
			return nil, nil
		}
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
			if oldPid == os.Getpid() {
				return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", oldPid)), 0644)
			}
			if processAlive(oldPid) {
				return fmt.Errorf("warden is already running (pid %d), use -r to reload", oldPid)
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
