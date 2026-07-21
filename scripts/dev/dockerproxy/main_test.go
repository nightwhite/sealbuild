package main

import "testing"

func TestTransformDockerProxy(t *testing.T) {
	tests := []struct {
		input     string
		want      string
		wantError bool
	}{
		{input: "http://127.0.0.1:7890", want: "http://host.docker.internal:7890"},
		{input: "http://localhost:7890", want: "http://host.docker.internal:7890"},
		{input: "http://[::1]:7890", want: "http://host.docker.internal:7890"},
		{input: "https://proxy.example:8443/path", want: "https://proxy.example:8443/path"},
		{input: "socks5://127.0.0.1:7890", wantError: true},
		{input: "http://user:pass@127.0.0.1:7890", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			actual, err := transformDockerProxy(test.input)
			if test.wantError {
				if err == nil {
					t.Fatalf("transformDockerProxy() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("transformDockerProxy() error = %v", err)
			}
			if actual != test.want {
				t.Fatalf("transformDockerProxy() = %q, want %q", actual, test.want)
			}
		})
	}
}
