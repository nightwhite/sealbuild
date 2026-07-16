package main

import (
	"os"

	"github.com/labring/sealbuild/internal/cli"
	"github.com/labring/sealbuild/internal/version"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version.Current()))
}
