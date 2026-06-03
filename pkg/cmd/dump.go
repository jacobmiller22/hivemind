package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

var dumpFlag *flag.FlagSet = flag.NewFlagSet("dump", flag.ContinueOnError)

type DumpConfig struct {
	OutputPath string
}

func parseDumpArgs(args []string) (*DumpConfig, error) {

	var cfg DumpConfig
	dumpFlag.StringVar(&cfg.OutputPath, "output", "/dev/fd/1", "The path where to dump out the configuration")

	if err := dumpFlag.Parse(args); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Dump() outputs the current configuration
func Dump(ctx context.Context, args []string) error {

	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.DebugContext(ctx, logkeys.CommandStart, logkeys.Command, "HMCONFIG_DUMP", logkeys.Config, cfg)

	dumpCfg, err := parseDumpArgs(args)
	if err != nil {
		return fmt.Errorf("error reading dump args: %w", err)
	}

	// Open file for dumping, creating if not exists
	// O_RDWR   → read/write
	// O_CREATE → create if not exists
	// O_APPEND → append (optional, remove if you don't want append mode)
	// 0644     → file permissions (rw-r--r--)
	f, err := os.OpenFile(dumpCfg.OutputPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if _, err := f.WriteString(string(data)); err != nil {
		return err
	}

	return nil
}
