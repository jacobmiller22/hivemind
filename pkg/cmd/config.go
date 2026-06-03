package cmd

import (
	"context"
	"fmt"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

/**
 * config.go defines the command line function `hivemind config`.
 * It is used to view + modify the application configuration
 */
const hmConfigUsage string = "Usage:\n\thmconfig [dump] [args]"

func Config(ctx context.Context, args []string) error {

	l := clog.FromContext(ctx)
	cfg := config.LoadConfig(args)

	l.DebugContext(ctx, logkeys.CommandStart, logkeys.Command, "HMCONFIG", logkeys.Config, cfg)

	return fmt.Errorf(hmConfigUsage)
}
