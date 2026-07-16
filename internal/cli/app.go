package cli

import (
	"fmt"
	"io"

	"github.com/labring/sealbuild/internal/version"
)

const usage = `Usage:
  sealbuild <command>

Commands:
  version    Print Sealbuild version information
`

func Run(args []string, stdout io.Writer, stderr io.Writer, info version.Info) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		if _, err := io.WriteString(stdout, usage); err != nil {
			return 1
		}
		return 0
	}

	if args[0] == "version" {
		if _, err := io.WriteString(stdout, info.String()); err != nil {
			return 1
		}
		return 0
	}

	if _, err := fmt.Fprintf(
		stderr,
		"sealbuild: unknown command %q\nRun 'sealbuild help' for usage.\n",
		args[0],
	); err != nil {
		return 1
	}
	return 2
}
