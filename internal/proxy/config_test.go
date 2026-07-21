package proxy

import (
	"os"
	"strings"
	"testing"
)

func TestParseValidatesAndTransformsExplicitProxy(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantGuest string
		wantError string
	}{
		{name: "remote HTTP", raw: "http://proxy.example:8080", wantGuest: "http://proxy.example:8080"},
		{name: "remote HTTPS", raw: "https://proxy.example:8443/path", wantGuest: "https://proxy.example:8443/path"},
		{name: "IPv4 loopback", raw: "http://127.0.0.1:7890", wantGuest: "http://10.0.2.2:7890"},
		{name: "localhost", raw: "http://localhost:7890", wantGuest: "http://10.0.2.2:7890"},
		{name: "IPv6 loopback", raw: "http://[::1]:7890", wantGuest: "http://10.0.2.2:7890"},
		{name: "empty", raw: "", wantError: "proxy URL is required"},
		{name: "whitespace", raw: " http://proxy.example", wantError: "surrounding whitespace"},
		{name: "unsupported scheme", raw: "socks5://proxy.example", wantError: "scheme must be http or https"},
		{name: "missing host", raw: "http:///proxy", wantError: "host is required"},
		{name: "userinfo", raw: "http://user:pass@proxy.example", wantError: "must not contain userinfo"},
		{name: "query", raw: "http://proxy.example?q=x", wantError: "must not contain a query"},
		{name: "fragment", raw: "http://proxy.example#x", wantError: "must not contain a fragment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := Parse(test.raw)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Parse() error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if config.Raw != test.raw || config.Guest != test.wantGuest {
				t.Fatalf("Parse() = %#v, want Raw %q Guest %q", config, test.raw, test.wantGuest)
			}
		})
	}
}

func TestParseDoesNotReadProxyEnvironment(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(name, "http://environment.example:8080")
	}
	if _, err := Parse(""); err == nil || !strings.Contains(err.Error(), "proxy URL is required") {
		t.Fatalf("Parse(\"\") error = %v, want required", err)
	}
}

func TestWriteGuestFileCreatesPrivateTemporaryMaterial(t *testing.T) {
	config, err := Parse("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	filePath, cleanup, err := config.WriteGuestFile(t.TempDir())
	if err != nil {
		t.Fatalf("WriteGuestFile() error = %v", err)
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "http://10.0.2.2:7890" {
		t.Fatalf("contents = %q", contents)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %#o, want 0600", info.Mode().Perm())
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("repeated cleanup() error = %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("Stat(after cleanup) error = %v, want not exist", err)
	}
}
