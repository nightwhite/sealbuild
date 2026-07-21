// Package proxy validates explicit Sealbuild HTTP proxy configuration.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Config preserves the explicit Host URL and its Guest-visible equivalent.
type Config struct {
	Raw   string
	Guest string
}

// Parse validates one explicit proxy URL without reading process environment.
func Parse(raw string) (Config, error) {
	if raw == "" {
		return Config{}, fmt.Errorf("proxy URL is required")
	}
	if len(raw) > 4096 {
		return Config{}, fmt.Errorf("proxy URL must not exceed 4096 bytes")
	}
	if strings.TrimSpace(raw) != raw {
		return Config{}, fmt.Errorf("proxy URL must not contain surrounding whitespace")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Config{}, fmt.Errorf("proxy URL is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Config{}, fmt.Errorf("proxy URL scheme must be http or https")
	}
	if parsed.Hostname() == "" {
		return Config{}, fmt.Errorf("proxy URL host is required")
	}
	if parsed.User != nil {
		return Config{}, fmt.Errorf("proxy URL must not contain userinfo")
	}
	if parsed.RawQuery != "" {
		return Config{}, fmt.Errorf("proxy URL must not contain a query")
	}
	if parsed.Fragment != "" {
		return Config{}, fmt.Errorf("proxy URL must not contain a fragment")
	}

	guestURL := *parsed
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		if parsed.Port() == "" {
			guestURL.Host = "10.0.2.2"
		} else {
			guestURL.Host = net.JoinHostPort("10.0.2.2", parsed.Port())
		}
	}
	return Config{Raw: raw, Guest: guestURL.String()}, nil
}

// Redacted returns a credential-free representation suitable for diagnostics.
func (config Config) Redacted() string {
	return config.Guest
}

// WriteGuestFile writes the Guest URL to a private temporary fw_cfg file.
func (config Config) WriteGuestFile(directory string) (path string, cleanup func() error, returnErr error) {
	if config.Guest == "" {
		return "", nil, fmt.Errorf("Guest proxy URL is required")
	}
	file, err := os.CreateTemp(directory, ".sealbuild-proxy-*.tmp")
	if err != nil {
		return "", nil, fmt.Errorf("create Guest proxy file: %w", err)
	}
	filePath := file.Name()
	removeFile := true
	defer func() {
		if removeFile {
			returnErr = errors.Join(returnErr, os.Remove(filePath))
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", nil, fmt.Errorf("set Guest proxy file permissions: %w", err)
	}
	if _, err := file.WriteString(config.Guest); err != nil {
		_ = file.Close()
		return "", nil, fmt.Errorf("write Guest proxy file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", nil, fmt.Errorf("sync Guest proxy file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", nil, fmt.Errorf("close Guest proxy file: %w", err)
	}
	removeFile = false

	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup = func() error {
		cleanupOnce.Do(func() {
			cleanupErr = os.Remove(filePath)
			if errors.Is(cleanupErr, os.ErrNotExist) {
				cleanupErr = nil
			}
		})
		return cleanupErr
	}
	return filePath, cleanup, nil
}
