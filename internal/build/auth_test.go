package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moby/buildkit/session/auth"
)

func TestAnonymousAuthFetchesTokenDirectlyWithoutEnvironmentProxy(t *testing.T) {
	var environmentProxyCalls atomic.Int32
	environmentProxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		environmentProxyCalls.Add(1)
	}))
	defer environmentProxy.Close()
	t.Setenv("HTTP_PROXY", environmentProxy.URL)
	t.Setenv("HTTPS_PROXY", environmentProxy.URL)
	t.Setenv("http_proxy", environmentProxy.URL)
	t.Setenv("https_proxy", environmentProxy.URL)

	tokenServer := tokenTestServer(t, "direct-token")
	defer tokenServer.Close()
	provider := anonymousProvider(t, "")
	response, err := provider.FetchToken(t.Context(), tokenRequest(tokenServer.URL))
	if err != nil {
		t.Fatalf("FetchToken() error = %v", err)
	}
	if response.Token != "direct-token" || response.ExpiresIn != 300 || response.IssuedAt == 0 {
		t.Fatalf("FetchToken() response = %#v", response)
	}
	if environmentProxyCalls.Load() != 0 {
		t.Fatalf("environment proxy calls = %d, want 0", environmentProxyCalls.Load())
	}
}

func TestAnonymousAuthUsesOnlyExplicitProxy(t *testing.T) {
	var proxyCalls atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		proxyCalls.Add(1)
		if request.URL.String() != "http://registry-token.invalid/token?scope=repository%3Alibrary%2Falpine%3Apull&service=registry.invalid" {
			t.Errorf("proxy request URL = %q", request.URL.String())
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{"token":"proxy-token","expires_in":120,"issued_at":"2026-07-17T06:00:00Z"}`)
	}))
	defer proxyServer.Close()

	provider := anonymousProvider(t, proxyServer.URL)
	response, err := provider.FetchToken(t.Context(), tokenRequest("http://registry-token.invalid/token"))
	if err != nil {
		t.Fatalf("FetchToken() error = %v", err)
	}
	if response.Token != "proxy-token" || proxyCalls.Load() != 1 {
		t.Fatalf("response = %#v proxy calls = %d", response, proxyCalls.Load())
	}
}

func TestAnonymousAuthReturnsEmptyCredentials(t *testing.T) {
	provider := anonymousProvider(t, "")
	response, err := provider.Credentials(t.Context(), &auth.CredentialsRequest{Host: "registry-1.docker.io"})
	if err != nil {
		t.Fatalf("Credentials() error = %v", err)
	}
	if response.Username != "" || response.Secret != "" {
		t.Fatalf("Credentials() = %#v, want anonymous", response)
	}
}

func TestAnonymousAuthRejectsInvalidProxyAndRealm(t *testing.T) {
	for _, proxyURL := range []string{"socks5://127.0.0.1:7890", "http://user:pass@127.0.0.1:7890", "http://127.0.0.1:7890?q=x"} {
		if _, err := NewAnonymousAuth(proxyURL); err == nil {
			t.Errorf("NewAnonymousAuth(%q) error = nil, want error", proxyURL)
		}
	}
	provider := anonymousProvider(t, "")
	for _, realm := range []string{"file:///tmp/token", "ftp://token.example/token", "://invalid"} {
		_, err := provider.FetchToken(t.Context(), tokenRequest(realm))
		if err == nil || !strings.Contains(err.Error(), "token realm") {
			t.Errorf("FetchToken(realm %q) error = %v, want token realm error", realm, err)
		}
	}
}

func anonymousProvider(t *testing.T, proxyURL string) *anonymousAuth {
	t.Helper()
	attachable, err := NewAnonymousAuth(proxyURL)
	if err != nil {
		t.Fatalf("NewAnonymousAuth() error = %v", err)
	}
	provider, ok := attachable.(*anonymousAuth)
	if !ok {
		t.Fatalf("NewAnonymousAuth() type = %T", attachable)
	}
	return provider
}

func tokenRequest(realm string) *auth.FetchTokenRequest {
	return &auth.FetchTokenRequest{
		Host: "registry-1.docker.io", Realm: realm, Service: "registry.invalid",
		Scopes: []string{"repository:library/alpine:pull"},
	}
}

func tokenTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("service") != "registry.invalid" {
			t.Errorf("service = %q", request.URL.Query().Get("service"))
		}
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(response, `{"token":%q,"expires_in":300,"issued_at":"2026-07-17T06:00:00Z"}`, token)
	}))
}
