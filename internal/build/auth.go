package build

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	authutil "github.com/containerd/containerd/v2/core/remotes/docker/auth"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth"
	"google.golang.org/grpc"

	proxyconfig "github.com/labring/sealbuild/internal/proxy"
)

type anonymousAuth struct {
	auth.UnimplementedAuthServer
	client *http.Client
}

// NewAnonymousAuth creates a credential-free Registry token session with one explicit proxy.
func NewAnonymousAuth(proxyURL string) (session.Attachable, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unsupported type %T", http.DefaultTransport)
	}
	clientTransport := transport.Clone()
	clientTransport.Proxy = nil
	if proxyURL != "" {
		config, err := proxyconfig.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("validate Host proxy: %w", err)
		}
		parsed, err := url.Parse(config.Raw)
		if err != nil {
			return nil, fmt.Errorf("parse validated Host proxy: %w", err)
		}
		clientTransport.Proxy = http.ProxyURL(parsed)
	}
	return &anonymousAuth{client: &http.Client{Transport: clientTransport}}, nil
}

func (provider *anonymousAuth) Register(server *grpc.Server) {
	auth.RegisterAuthServer(server, provider)
}

func (provider *anonymousAuth) Credentials(context.Context, *auth.CredentialsRequest) (*auth.CredentialsResponse, error) {
	return &auth.CredentialsResponse{}, nil
}

func (provider *anonymousAuth) FetchToken(ctx context.Context, request *auth.FetchTokenRequest) (*auth.FetchTokenResponse, error) {
	realm, err := url.Parse(request.Realm)
	if err != nil || (realm.Scheme != "http" && realm.Scheme != "https") || realm.Host == "" {
		return nil, fmt.Errorf("Registry token realm must be an absolute HTTP or HTTPS URL")
	}
	response, err := authutil.FetchToken(ctx, provider.client, nil, authutil.TokenOptions{
		Realm: request.Realm, Service: request.Service, Scopes: request.Scopes,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch anonymous Registry token: %w", err)
	}
	result := &auth.FetchTokenResponse{
		Token:     response.Token,
		ExpiresIn: int64(response.ExpiresInSeconds),
	}
	if !response.IssuedAt.IsZero() {
		result.IssuedAt = response.IssuedAt.Unix()
	}
	return result, nil
}
