package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	buildpkg "github.com/labring/sealbuild/internal/build"
	proxyconfig "github.com/labring/sealbuild/internal/proxy"
	"github.com/labring/sealbuild/internal/version"
)

const usage = `Usage:
  sealbuild build --output PATH [options] CONTEXT
  sealbuild version

Commands:
  build      Build a linux/amd64 OCI image archive
  version    Print Sealbuild version information
`

// Builder executes one validated CLI build request.
type Builder interface {
	Build(context.Context, buildpkg.Request, io.Writer) error
}

// Run executes one Sealbuild CLI invocation and returns its process exit code.
func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, info version.Info, builder Builder) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		if _, err := io.WriteString(stdout, usage); err != nil {
			return 1
		}
		return 0
	}

	switch args[0] {
	case "version":
		if _, err := io.WriteString(stdout, info.String()); err != nil {
			return 1
		}
		return 0
	case "build":
		return runBuild(ctx, args[1:], stdout, stderr, builder)
	default:
		if _, err := fmt.Fprintf(stderr, "sealbuild: unknown command %q\nRun 'sealbuild help' for usage.\n", args[0]); err != nil {
			return 1
		}
		return 2
	}
}

func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer, builder Builder) int {
	flags := flag.NewFlagSet("sealbuild build", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var dockerfile string
	var outputPath string
	var proxyURL string
	var noProxy string
	var buildArguments stringList
	flags.StringVar(&dockerfile, "dockerfile", "", "Dockerfile path relative to context")
	flags.StringVar(&outputPath, "output", "", "OCI archive output path")
	flags.StringVar(&proxyURL, "proxy", "", "explicit HTTP or HTTPS proxy")
	flags.StringVar(&noProxy, "no-proxy", "", "comma-separated hosts that bypass the proxy")
	flags.Var(&buildArguments, "build-arg", "Dockerfile build argument NAME=VALUE")
	if err := flags.Parse(args); err != nil {
		return argumentError(stderr, "invalid build arguments: %v", err)
	}
	if flags.NArg() != 1 {
		return argumentError(stderr, "build requires exactly one context directory")
	}
	if outputPath == "" {
		return argumentError(stderr, "build requires --output PATH")
	}
	noProxySet := false
	flags.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "no-proxy" {
			noProxySet = true
		}
	})
	if noProxySet && noProxy == "" {
		return argumentError(stderr, "--no-proxy must not be empty")
	}
	parsedBuildArgs := make(map[string]string, len(buildArguments))
	for _, argument := range buildArguments {
		parts := strings.SplitN(argument, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return argumentError(stderr, "invalid --build-arg %q; expected NAME=VALUE", argument)
		}
		if _, exists := parsedBuildArgs[parts[0]]; exists {
			return argumentError(stderr, "duplicate --build-arg %q", parts[0])
		}
		parsedBuildArgs[parts[0]] = parts[1]
	}
	if noProxySet {
		for _, name := range []string{"NO_PROXY", "no_proxy"} {
			if _, exists := parsedBuildArgs[name]; exists {
				return argumentError(stderr, "--no-proxy conflicts with --build-arg %s", name)
			}
			parsedBuildArgs[name] = noProxy
		}
	}
	var proxy *proxyconfig.Config
	if proxyURL != "" {
		parsed, err := proxyconfig.Parse(proxyURL)
		if err != nil {
			return argumentError(stderr, "invalid --proxy: %v", err)
		}
		proxy = &parsed
	}
	if builder == nil {
		if _, err := fmt.Fprintln(stderr, "sealbuild: build is unavailable because Runtime is not configured"); err != nil {
			return 1
		}
		return 1
	}
	request := buildpkg.Request{
		ContextDir: flags.Arg(0), Dockerfile: dockerfile, OutputPath: outputPath,
		BuildArgs: parsedBuildArgs, Proxy: proxy,
	}
	if err := builder.Build(ctx, request, stdout); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "sealbuild: build failed: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func argumentError(stderr io.Writer, format string, arguments ...any) int {
	if _, err := fmt.Fprintf(stderr, "sealbuild: "+format+"\n", arguments...); err != nil {
		return 1
	}
	return 2
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}
