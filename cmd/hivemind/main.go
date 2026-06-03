package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jacobmiller22/gossentials/clog"
	"github.com/jacobmiller22/gossentials/treecli"

	"github.com/jacobmiller22/hivemind/pkg/cmd"
	"github.com/jacobmiller22/hivemind/pkg/config"
	"github.com/jacobmiller22/hivemind/pkg/logkeys"
)

const usage string = "Usage: hivemind"

func main() {

	ctx := context.Background()

	cfg := config.LoadConfig(os.Args)

	l, err := cfg.Logger()
	if err != nil {
		fmt.Printf("Error creating logger: %v\n", err)
		os.Exit(1)
	}

	ctx = clog.WithContext(ctx, l)

	if err := config.InitDefaultConfig(); err != nil && !errors.Is(err, config.ErrConfigExists) {
		l.InfoContext(ctx, "could not create default config", logkeys.Error, err)
	}

	namespace := os.Args[0]

	hivemind := treecli.New(namespace, cmd.Client,
		treecli.WithChildren(
			[]treecli.Node{
				*treecli.New("hook", cmd.Hook,
					treecli.WithChildren(
						[]treecli.Node{
							*treecli.New("antigravity2.0", cmd.HookAntigravity20),
						},
					),
				),
				*treecli.New("event", cmd.Event),
				*treecli.New("daemon", cmd.Daemon),
				*treecli.New("config", cmd.Config,
					treecli.WithChildren(
						[]treecli.Node{
							*treecli.New("dump", cmd.Dump),
						},
					),
				),
			},
		),
	)

	target, targetArgs := hivemind.Search(os.Args)

	if target == nil {
		if len(os.Args) > 1 && len(os.Args[1]) > 0 && os.Args[1][0] == '-' {
			target = hivemind
			targetArgs = os.Args[1:]
		} else {
			l.ErrorContext(ctx, "target not found", "usage", usage, "target", target, "targetArgs", targetArgs)
			os.Exit(1)
		}
	}

	if err := target.Entrypoint(ctx, targetArgs); err != nil {
		if errors.Is(err, treecli.ErrNoEntrypoint) {
			fmt.Println(usage)
		} else {
			fmt.Println(err)
		}
		os.Exit(0)
	}

	return
}
