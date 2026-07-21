// Package build manages BuildKit requests for fixed linux/amd64 OCI builds.
package build

import (
	"context"
	"errors"
	"fmt"

	buildkit "github.com/moby/buildkit/client"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

const buildKitServerName = "sealbuild-runtime"

// WorkerClient is the BuildKit surface needed by the VM readiness probe.
type WorkerClient interface {
	ListWorkers(context.Context, ...buildkit.ListWorkersOption) ([]*buildkit.WorkerInfo, error)
	Close() error
}

// ClientFactory opens one mTLS BuildKit client.
type ClientFactory func(context.Context, string, tlsmaterial.Paths) (WorkerClient, error)

// Probe verifies that one BuildKit daemon exposes only an AMD64 OCI worker.
type Probe struct {
	Open ClientFactory
}

// Ready implements vm.Probe.
func (probe Probe) Ready(ctx context.Context, address string, tls tlsmaterial.Paths) (returnErr error) {
	if probe.Open == nil {
		return fmt.Errorf("BuildKit client factory is required")
	}
	client, err := probe.Open(ctx, address, tls)
	if err != nil {
		return fmt.Errorf("open BuildKit client: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, client.Close())
	}()
	workers, err := client.ListWorkers(ctx)
	if err != nil {
		return fmt.Errorf("list BuildKit workers: %w", err)
	}
	return ValidateWorkers(workers)
}

// OpenBuildKitClient opens the product mTLS connection to one Guest BuildKit daemon.
func OpenBuildKitClient(ctx context.Context, address string, tls tlsmaterial.Paths) (WorkerClient, error) {
	client, err := buildkit.New(
		ctx,
		address,
		buildkit.WithServerConfig(buildKitServerName, tls.CA),
		buildkit.WithCredentials(tls.ClientCert, tls.ClientKey),
	)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// ValidateWorkers enforces the single fixed linux/amd64 worker contract.
func ValidateWorkers(workers []*buildkit.WorkerInfo) error {
	if len(workers) != 1 {
		return fmt.Errorf("BuildKit must expose exactly one worker, got %d", len(workers))
	}
	if workers[0] == nil {
		return fmt.Errorf("BuildKit worker must not be nil")
	}
	hasBaseline := false
	for _, platform := range workers[0].Platforms {
		if platform.OS != "linux" || platform.Architecture != "amd64" {
			return fmt.Errorf("BuildKit worker exposes unsupported platform %s/%s", platform.OS, platform.Architecture)
		}
		if platform.Variant == "" {
			hasBaseline = true
		}
	}
	if !hasBaseline {
		return fmt.Errorf("BuildKit worker must expose baseline linux/amd64")
	}
	return nil
}
