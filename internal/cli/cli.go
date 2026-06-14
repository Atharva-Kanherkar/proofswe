package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

var ErrUsage = errors.New("usage: proofswe <enable|disable|off|on|status|consent|show|inspect|resolve|hook|version|help>")

type Config struct {
	Args      []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	BuildInfo *debug.BuildInfo
	HomeDir   string
	WorkDir   string
	ExePath   string
	Getenv    func(string) string
}

func Run(ctx context.Context, cfg Config) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}

	if len(cfg.Args) == 0 {
		return printUsage(cfg.Stdout)
	}

	if cfg.Args[0] == "hook" {
		return runHook(ctx, cfg, cfg.Args[1:])
	}
	cfg = cfg.withDefaults()

	switch cfg.Args[0] {
	case "help", "-h", "--help":
		return printUsage(cfg.Stdout)
	case "enable":
		return enableHooks(cfg)
	case "disable":
		return disableHooks(cfg, cfg.Args[1:])
	case "off":
		return setEnabled(cfg, false)
	case "on":
		return setEnabled(cfg, true)
	case "status":
		return printStatus(cfg)
	case "consent":
		return runConsentCommand(cfg, cfg.Args[1:])
	case "show", "inspect":
		return runShowCommand(cfg, cfg.Args[1:])
	case "resolve":
		return runResolveCommand(cfg, cfg.Args[1:])
	case "version", "-v", "--version":
		return printVersion(cfg.Stdout, cfg.Version, cfg.BuildInfo)
	default:
		return fmt.Errorf("%w: unknown command %q", ErrUsage, cfg.Args[0])
	}
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `proofswe captures local coding-agent session metadata.

Usage:
  proofswe enable
  proofswe disable --hooks
  proofswe off
  proofswe on
  proofswe status
  proofswe consent show
  proofswe consent set --tier=<hashes-only|prompts|actions|code|full>
  proofswe show <session>
  proofswe inspect <session>
  proofswe resolve [--maturity=24h]
  proofswe hook <claudecode|codex> <SessionStart|SessionEnd|Stop>
  proofswe version
  proofswe help
`)
	return err
}

func (cfg Config) withDefaults() Config {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.HomeDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.HomeDir = home
		}
	}
	if cfg.WorkDir == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.WorkDir = wd
		}
	}
	if cfg.ExePath == "" {
		if exe, err := os.Executable(); err == nil {
			cfg.ExePath = exe
		}
	}
	return cfg
}

func printVersion(w io.Writer, version string, info *debug.BuildInfo) error {
	if version == "" {
		version = "dev"
	}

	if info == nil {
		_, err := fmt.Fprintf(w, "proofswe %s\n", version)
		return err
	}

	revision := buildSetting(info, "vcs.revision")
	if revision == "" {
		_, err := fmt.Fprintf(w, "proofswe %s\n", version)
		return err
	}

	modified := buildSetting(info, "vcs.modified")
	if modified == "true" {
		revision += "-dirty"
	}

	_, err := fmt.Fprintf(w, "proofswe %s (%s)\n", version, revision)
	return err
}

func buildSetting(info *debug.BuildInfo, key string) string {
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}

	return ""
}
