package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
)

var ErrUsage = errors.New("usage: proofswe <status|version|help>")

type Config struct {
	Args      []string
	Stdout    io.Writer
	Version   string
	BuildInfo *debug.BuildInfo
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

	if len(cfg.Args) == 0 {
		return printUsage(cfg.Stdout)
	}

	switch cfg.Args[0] {
	case "help", "-h", "--help":
		return printUsage(cfg.Stdout)
	case "status":
		_, err := fmt.Fprintln(cfg.Stdout, "proofswe scaffold ready")
		return err
	case "version", "-v", "--version":
		return printVersion(cfg.Stdout, cfg.Version, cfg.BuildInfo)
	default:
		return fmt.Errorf("%w: unknown command %q", ErrUsage, cfg.Args[0])
	}
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `proofswe captures local coding-agent session metadata.

Usage:
  proofswe status
  proofswe version
  proofswe help
`)
	return err
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
