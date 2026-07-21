package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
