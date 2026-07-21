package main

import (
	"strings"
	"testing"
)

func TestTransformProxyURLForGuest(t *testing.T) {
	tests := []struct {
		name      string
		proxyURL  string
		want      string
		wantError string
	}{
		{name: "remote HTTP", proxyURL: "http://proxy.example:8080", want: "http://proxy.example:8080"},
		{name: "loopback IPv4", proxyURL: "http://127.0.0.1:7890", want: "http://10.0.2.2:7890"},
		{name: "localhost HTTPS", proxyURL: "https://localhost:8443", want: "https://10.0.2.2:8443"},
		{name: "loopback IPv6", proxyURL: "http://[::1]:7890", want: "http://10.0.2.2:7890"},
		{name: "unsupported scheme", proxyURL: "socks5://proxy.example:1080", wantError: "scheme must be http or https"},
		{name: "userinfo", proxyURL: "http://user:pass@proxy.example:8080", wantError: "must not contain userinfo"},
		{name: "query", proxyURL: "http://proxy.example:8080?token=x", wantError: "must not contain a query"},
		{name: "fragment", proxyURL: "http://proxy.example:8080#fragment", wantError: "must not contain a fragment"},
		{name: "missing host", proxyURL: "http:///proxy", wantError: "host is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := transformProxyURL(strings.NewReader(test.proxyURL))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("transformProxyURL() error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("transformProxyURL() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("transformProxyURL() = %q, want %q", got, test.want)
			}
		})
	}
}
