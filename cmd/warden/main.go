package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/sower-proxy/feconf"
	_ "github.com/sower-proxy/feconf/decoder/yaml"
	_ "github.com/sower-proxy/feconf/reader/file"
	_ "github.com/sower-proxy/feconf/reader/http"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/app"
	"github.com/wweir/warden/internal/install"
)

var Version, BuildTime string

var configCandidates = []string{
	"warden.yaml", "warden.yml",
	"config/warden.yaml", "config/warden.yml",
	"/etc/warden.yaml", "/etc/warden.yml",
}

func main() {
	// parse command-line flags
	installService := flag.Bool("s", false, "install as systemd service")

	// load configuration
	cfg, err := feconf.New[config.ConfigStruct]("c",
		configCandidates...,
	).Parse()
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

	application := app.NewApp(cfg, configPath)
	if err := application.Run(); err != nil {
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
