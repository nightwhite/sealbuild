package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	buildpkg "github.com/labring/sealbuild/internal/build"
	"github.com/labring/sealbuild/internal/cache"
	"github.com/labring/sealbuild/internal/cli"
	"github.com/labring/sealbuild/internal/runtimeassets"
	"github.com/labring/sealbuild/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var builder cli.Builder
	if len(args) > 0 && args[0] == "build" {
		bundle, err := runtimeassets.Bundle()
		if err != nil {
			if _, writeErr := fmt.Fprintf(os.Stderr, "sealbuild: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		layout, err := cache.DefaultLayout()
		if err != nil {
			if _, writeErr := fmt.Fprintf(os.Stderr, "sealbuild: configure cache: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		runner := buildpkg.NewRunner(bundle, layout)
		builder = runner
	}
	return cli.Run(ctx, args, os.Stdout, os.Stderr, version.Current(), builder)
}
