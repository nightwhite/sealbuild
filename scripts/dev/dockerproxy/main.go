package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	proxyconfig "github.com/labring/sealbuild/internal/proxy"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s PROXY_URL\n", os.Args[0])
		os.Exit(2)
	}
	transformed, err := transformDockerProxy(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate Docker development proxy: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(transformed)
}

func transformDockerProxy(raw string) (string, error) {
	config, err := proxyconfig.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(config.Raw)
	if err != nil {
		return "", fmt.Errorf("parse validated proxy URL: %w", err)
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		if parsed.Port() == "" {
			parsed.Host = "host.docker.internal"
		} else {
			parsed.Host = net.JoinHostPort("host.docker.internal", parsed.Port())
		}
	}
	return parsed.String(), nil
}
